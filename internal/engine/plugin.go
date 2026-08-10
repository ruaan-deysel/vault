//go:build linux

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ruaan-deysel/vault/internal/dedup"
)

// pluginsDir is the base directory where Unraid plugins are installed.
const pluginsDir = "/boot/config/plugins"

// pluginPath returns the canonical config directory for a plugin under
// /boot/config/plugins/. Shared by Backup (tar) and BackupChunked (dedup) so
// both code paths agree on what the plugin's "data" lives in.
func pluginPath(name string) string {
	if name == "" {
		return ""
	}
	return filepath.Join(pluginsDir, name)
}

// PluginHandler implements Handler for Unraid plugin backup/restore.
// Each plugin consists of a .plg installer file and an optional per-plugin
// configuration directory under /boot/config/plugins/<name>/.
type PluginHandler struct{}

// NewPluginHandler creates a new PluginHandler.
func NewPluginHandler() (*PluginHandler, error) {
	if _, err := os.Stat(pluginsDir); err != nil {
		return nil, fmt.Errorf("plugins directory not accessible: %w", err)
	}
	return &PluginHandler{}, nil
}

// ListItems scans /boot/config/plugins/ for .plg files and returns each
// as a BackupItem. The item name is the plugin name (filename without .plg).
func (h *PluginHandler) ListItems() ([]BackupItem, error) {
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return nil, fmt.Errorf("reading plugins directory: %w", err)
	}

	var items []BackupItem
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".plg") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".plg")
		plgPath := filepath.Join(pluginsDir, entry.Name())

		// Check if a per-plugin config directory exists.
		configDir := filepath.Join(pluginsDir, name)
		hasConfig := false
		if info, statErr := os.Stat(configDir); statErr == nil && info.IsDir() {
			hasConfig = true
		}

		items = append(items, BackupItem{
			Name: name,
			Type: "plugin",
			Settings: map[string]any{
				"id":         name,
				"plg_path":   plgPath,
				"config_dir": configDir,
				"has_config": hasConfig,
			},
		})
	}
	return items, nil
}

// Backup creates a tar.gz archive containing the plugin's .plg file and its
// configuration directory (if it exists).
func (h *PluginHandler) Backup(ctx context.Context, item BackupItem, destDir string, progress ProgressFunc) (*BackupResult, error) {
	result := &BackupResult{ItemName: item.Name}

	pluginName, _ := item.Settings["id"].(string)
	if pluginName == "" {
		pluginName = item.Name
	}

	// Validate plugin name to prevent path traversal (CWE-22).
	safePluginName, err := normalizeRestoreComponent(pluginName)
	if err != nil {
		return nil, fmt.Errorf("invalid plugin name: %w", err)
	}
	pluginName = safePluginName

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("creating dest dir: %w", err)
	}

	// Step 1: Copy the .plg file.
	progress(item.Name, 10, "copying plugin file")
	plgSrc := filepath.Join(pluginsDir, pluginName+".plg")
	plgDst := filepath.Join(destDir, pluginName+".plg")
	if _, err := os.Stat(plgSrc); err != nil {
		return nil, fmt.Errorf("plugin file not found: %w", err)
	}
	data, err := os.ReadFile(plgSrc) // #nosec G304 — pluginName validated by normalizeRestoreComponent above
	if err != nil {
		return nil, fmt.Errorf("reading plugin file: %w", err)
	}
	if err := os.WriteFile(plgDst, data, 0644); err != nil {
		return nil, fmt.Errorf("writing plugin file: %w", err)
	}
	result.Files = append(result.Files, backupFileInfo(plgDst))

	// Step 2: Archive the config directory if it exists.
	progress(item.Name, 40, "archiving config")
	configDir := pluginPath(pluginName)
	if info, err := os.Stat(configDir); err == nil && info.IsDir() {
		effectiveCompression := MaybeDowngradeCompression(configDir, item.Compression)
		archivePath := filepath.Join(destDir, "config.tar"+archiveExt(effectiveCompression))
		if err := tarDirectory(ctx, configDir, archivePath, nil, effectiveCompression); err != nil {
			return nil, fmt.Errorf("archiving plugin config: %w", err)
		}
		result.Files = append(result.Files, backupFileInfo(archivePath))
		if err := WriteTarIndex(archivePath); err == nil {
			result.Files = append(result.Files, backupFileInfo(archivePath+IndexSuffix))
		}
	}

	// Step 3: Save metadata.
	progress(item.Name, 80, "saving metadata")
	meta := map[string]string{
		"name":       pluginName,
		"plg_file":   pluginName + ".plg",
		"config_dir": configDir,
	}
	metaJSON, _ := json.MarshalIndent(meta, "", "  ")
	metaPath := filepath.Join(destDir, "plugin_meta.json")
	if err := os.WriteFile(metaPath, metaJSON, 0644); err != nil {
		return nil, fmt.Errorf("writing plugin metadata: %w", err)
	}
	result.Files = append(result.Files, backupFileInfo(metaPath))

	progress(item.Name, 100, "backup complete")
	result.Success = true
	return result, nil
}

// Restore extracts the plugin's .plg file and config directory back to
// /boot/config/plugins/. The plugin will be recognized on next Unraid boot
// or when the Plugins page is refreshed.
func (h *PluginHandler) Restore(ctx context.Context, item BackupItem, sourceDir string, progress ProgressFunc) error {
	progress(item.Name, 10, "reading metadata")

	pluginName := item.Name

	// Try to read metadata for plugin name.
	metaPath := filepath.Join(sourceDir, "plugin_meta.json")
	if data, err := os.ReadFile(metaPath); err == nil { // #nosec G304 — metaPath is sourceDir (caller-controlled temp dir) + fixed filename
		var meta struct {
			Name string `json:"name"`
		}
		if jsonErr := json.Unmarshal(data, &meta); jsonErr == nil && meta.Name != "" {
			pluginName = meta.Name
		}
	}

	safePluginName, err := normalizeRestoreComponent(pluginName)
	if err != nil {
		return err
	}
	pluginName = safePluginName

	// Step 1: Restore the .plg file.
	progress(item.Name, 30, "restoring plugin file")
	plgSrc := filepath.Join(sourceDir, pluginName+".plg")
	if data, err := os.ReadFile(plgSrc); err == nil { // #nosec G304 — pluginName validated by normalizeRestoreComponent above
		plgDst := filepath.Join(pluginsDir, pluginName+".plg")
		if err := os.WriteFile(plgDst, data, 0644); err != nil {
			return fmt.Errorf("writing plugin file: %w", err)
		}
	} else {
		return fmt.Errorf("plugin file not found in backup: %w", err)
	}

	// Step 2: Restore config directory.
	progress(item.Name, 60, "restoring config")
	if configArchive, err := findArchive(sourceDir, "config.tar"); err == nil {
		configDir := pluginPath(pluginName)
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return fmt.Errorf("creating config dir: %w", err)
		}
		include := extractRestoreFilePaths(item.Settings)
		if err := untarDirectoryFiltered(ctx, configArchive, configDir, include); err != nil { // untarDirectoryFiltered inherits Zip Slip (CWE-22) protection via joinArchiveTarget + resolveWithinBase
			return fmt.Errorf("restoring plugin config: %w", err)
		}
	}

	progress(item.Name, 100, "restore complete")
	return nil
}

// pluginPlgFilePath resolves where the plugin's .plg installer lives.
// item.Settings["plg_path"] overrides the default (used by tests so they can
// point at a t.TempDir()); in production it is /boot/config/plugins/<name>.plg,
// matching the classic Backup/Restore path.
func pluginPlgFilePath(name string, settings map[string]any) string {
	if p, _ := settings["plg_path"].(string); p != "" {
		return p
	}
	if name == "" {
		return ""
	}
	return filepath.Join(pluginsDir, name+".plg")
}

// BackupChunked walks the plugin's config directory tree and chunks every
// regular file into the dedup repo, then stores the plugin's .plg installer
// file as a single reserved manifest entry so a dedup-only restore can
// reinstate it. Delegates the config directory to FolderHandler.BackupChunked
// — plugins are folder-shaped under /boot/config/plugins/<name>/.
//
// item.Settings["path"] overrides the default config path lookup and
// item.Settings["plg_path"] overrides the .plg lookup (both used by tests so
// they can point at a t.TempDir() instead of /boot/config/plugins/); in
// production the runner sets neither and the well-known paths apply.
//
// When no .plg file is present (tests, or a plugin without an installer) the
// manifest holds only the config files, exactly as the folder backup produced
// before this change, so existing restore points are unaffected.
func (h *PluginHandler) BackupChunked(ctx context.Context, item BackupItem, repo *dedup.Repo, progress ProgressFunc) (dedup.ID, error) {
	src, _ := item.Settings["path"].(string)
	if src == "" {
		src = pluginPath(item.Name)
	}
	if src == "" {
		return dedup.ID{}, fmt.Errorf("plugin: cannot resolve directory for %q", item.Name)
	}
	proxy := BackupItem{Name: item.Name, Type: "folder", Settings: map[string]any{"path": src}}
	fh := &FolderHandler{}
	m, _, _, err := fh.buildChunkedManifest(ctx, proxy, repo, progress)
	if err != nil {
		return dedup.ID{}, err
	}

	// Attach the .plg installer as a reserved single-chunk entry on the same
	// manifest (best-effort). Done before PutManifest because the config
	// manifest is not yet flushed and so cannot be read back mid-run.
	if plgData, rErr := os.ReadFile(pluginPlgFilePath(item.Name, item.Settings)); rErr == nil { // #nosec G304 — path derived from the plugin name / test override
		chunkID, pErr := repo.Put(plgData)
		if pErr != nil {
			return dedup.ID{}, fmt.Errorf("plugin: storing .plg: %w", pErr)
		}
		m.Files[PluginPlgManifestKey] = dedup.ManifestEntry{Size: int64(len(plgData)), Chunks: []dedup.ID{chunkID}}
	}
	return repo.PutManifest(item.Name, m)
}

// RestoreChunked reconstructs the plugin's config directory tree from the
// manifest and, when the backup captured one, restores the .plg installer
// file. destPath defaults to the plugin's well-known directory
// (pluginPath(item.Name)) when empty — the runner passes "" so production
// restore lands in /boot/config/plugins/<name>/. Tests pass a t.TempDir().
func (h *PluginHandler) RestoreChunked(ctx context.Context, item BackupItem, repo *dedup.Repo, manifestID dedup.ID, destPath string, progress ProgressFunc) error {
	if destPath == "" {
		destPath = pluginPath(item.Name)
	}
	if destPath == "" {
		return fmt.Errorf("plugin: cannot resolve restore directory for %q", item.Name)
	}
	// Propagate the partial-restore file picker through the proxy item so a
	// selection made on a dedup plugin restore reaches FolderHandler.
	// RestoreChunked, which honours restore_file_paths. Without this the
	// setting is dropped and the whole config directory is restored.
	proxySettings := map[string]any{"path": destPath}
	if rfp := extractRestoreFilePaths(item.Settings); rfp != nil {
		proxySettings["restore_file_paths"] = rfp
	}
	proxy := BackupItem{Name: item.Name, Type: "folder", Settings: proxySettings}
	fh := &FolderHandler{}
	// The config restore skips the reserved PluginPlgManifestKey entry, so the
	// .plg is never written into the config tree by the folder restore.
	if err := fh.RestoreChunked(ctx, proxy, repo, manifestID, destPath, progress); err != nil {
		return err
	}
	return h.restorePlgFromManifest(item, repo, manifestID)
}

// restorePlgFromManifest writes the plugin's .plg installer back to its
// well-known location when the manifest carries one. Manifests written before
// the .plg was captured simply have no such entry and this is a no-op.
func (h *PluginHandler) restorePlgFromManifest(item BackupItem, repo *dedup.Repo, manifestID dedup.ID) error {
	m, err := repo.GetManifest(manifestID)
	if err != nil {
		return err
	}
	entry, ok := m.Files[PluginPlgManifestKey]
	if !ok || len(entry.Chunks) == 0 {
		return nil
	}
	data, err := repo.Get(entry.Chunks[0])
	if err != nil {
		return fmt.Errorf("plugin: reading stored .plg: %w", err)
	}

	dst, _ := item.Settings["plg_path"].(string)
	if dst == "" {
		safeName, err := normalizeRestoreComponent(item.Name)
		if err != nil {
			return err
		}
		dst = filepath.Join(pluginsDir, safeName+".plg")
	}
	if err := os.WriteFile(dst, data, 0644); err != nil { // #nosec G306 — .plg is installed world-readable, matching classic Restore
		return fmt.Errorf("plugin: writing .plg: %w", err)
	}
	return nil
}
