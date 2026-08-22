package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MergeContainerChainStaging merges per-chain-step staging directories for a
// classic container restore into a single directory that
// ContainerHandler.Restore can consume. Sidecar files (config.json,
// template.xml, volumes.json) are taken from the newest step that has them;
// image.tar (whose compression suffix varies) is located via findArchive.
// Each volume_*.tar is the overlay of every step's archive, oldest first, so
// unchanged files from the base full backup survive a later partial
// differential/incremental archive (issue #320). stepDirs MUST be ordered
// oldest first.
func MergeContainerChainStaging(ctx context.Context, stepDirs []string, outDir string) error {
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return fmt.Errorf("creating merge dir: %w", err)
	}

	// Plain sidecars: newest step wins.
	for _, name := range []string{"config.json", "template.xml", "volumes.json"} {
		for i := len(stepDirs) - 1; i >= 0; i-- {
			src := filepath.Join(stepDirs[i], name)
			if _, err := os.Stat(src); err == nil {
				if err := mergeCopyFile(src, filepath.Join(outDir, name)); err != nil {
					return fmt.Errorf("merge sidecar %s: %w", name, err)
				}
				break
			}
		}
	}

	// image.tar: locate across steps (compression suffix varies).
	for i := len(stepDirs) - 1; i >= 0; i-- {
		if src, err := findArchive(stepDirs[i], "image.tar"); err == nil {
			if err := mergeCopyFile(src, filepath.Join(outDir, filepath.Base(src))); err != nil {
				return fmt.Errorf("merge image archive: %w", err)
			}
			break
		}
	}

	// Collect distinct volume archive base names (e.g. "volume_0.tar") across
	// every step, normalising the compression suffix.
	bases := map[string]struct{}{}
	for _, dir := range stepDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasPrefix(name, "volume_") {
				continue
			}
			base := name
			if i := strings.Index(base, ".tar"); i >= 0 {
				base = base[:i] + ".tar"
			}
			bases[base] = struct{}{}
		}
	}

	sorted := make([]string, 0, len(bases))
	for b := range bases {
		sorted = append(sorted, b)
	}
	sort.Strings(sorted)

	for _, base := range sorted {
		work := filepath.Join(outDir, base+".merge")
		if err := os.MkdirAll(work, 0o750); err != nil {
			return fmt.Errorf("creating work dir for %s: %w", base, err)
		}
		// Overlay each step's archive, oldest first. A step that did not
		// archive this volume (unchanged) simply has no matching archive.
		for _, dir := range stepDirs {
			archive, err := findArchive(dir, base)
			if err != nil {
				continue
			}
			if err := untarDirectory(ctx, archive, work); err != nil {
				return fmt.Errorf("extracting %s from %s: %w", base, dir, err)
			}
		}
		// Re-tar the merged tree as a plain archive; findArchive checks the
		// plain name first and untarDirectory auto-detects compression.
		if err := tarDirectory(ctx, work, filepath.Join(outDir, base), nil, CompressionNone); err != nil {
			return fmt.Errorf("creating merged %s: %w", base, err)
		}
		if err := os.RemoveAll(work); err != nil {
			return fmt.Errorf("removing work dir for %s: %w", base, err)
		}
	}
	return nil
}

func mergeCopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// TarDirectory is an exported wrapper for tarDirectory, used by the runner
// package's tests to stage volume archives without reaching into unexported
// engine helpers.
func TarDirectory(ctx context.Context, srcDir, destPath string, exclusions []string, compression string) error {
	return tarDirectory(ctx, srcDir, destPath, exclusions, compression)
}

// UntarDirectory is an exported wrapper for untarDirectory, used by the
// runner package's tests to inspect merged archives.
func UntarDirectory(ctx context.Context, srcPath, destDir string) error {
	return untarDirectory(ctx, srcPath, destDir)
}
