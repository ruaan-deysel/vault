package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruaan-deysel/vault/internal/dedup"
)

// backupTestVolume backs up a small source tree as a folder chunked backup
// and returns the sub-manifest ID. The caller owns repo cleanup.
func backupTestVolume(t *testing.T, repo *dedup.Repo, src string) dedup.ID {
	t.Helper()
	fh := &FolderHandler{}
	id, err := fh.BackupChunked(context.Background(), BackupItem{
		Name:     "vol",
		Type:     "folder",
		Settings: map[string]any{"path": src},
	}, repo, nil, nil)
	if err != nil {
		t.Fatalf("BackupChunked() error = %v", err)
	}
	if err := repo.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	return id
}

// inspectFromJSON builds a restoreInspect from a JSON string so tests avoid
// spelling out the anonymous Mounts struct literally.
func inspectFromJSON(t *testing.T, raw string) restoreInspect {
	t.Helper()
	var ins restoreInspect
	if err := json.Unmarshal([]byte(raw), &ins); err != nil {
		t.Fatalf("inspectFromJSON() error = %v", err)
	}
	return ins
}

func TestRestoreChunkedVolumes_CustomDestBindMount(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "config.yml"), []byte("foo: bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, _, cleanup := dedup.NewTestRepoForEngine(t)
	defer cleanup()

	subID := backupTestVolume(t, r, src)

	// Point the mount Source at a path that does NOT exist on disk.
	// With a custom restoreDest the data must land under restoreDest,
	// and the phantom source path must NOT be created.
	phantomSource := filepath.Join(t.TempDir(), "original")
	inspect := inspectFromJSON(t, fmt.Sprintf(`{
		"Name": "/test-container",
		"Config": {"Image": "nginx:latest"},
		"Mounts": [{"Type":"bind","Source":%q,"Destination":"/etc/myapp"}]
	}`, phantomSource))

	restoreDest := t.TempDir()
	m := dedup.Manifest{
		Files: map[string]dedup.ManifestEntry{
			containerVolPrefix + "/etc/myapp": {
				Size:   100,
				Chunks: []dedup.ID{subID},
			},
		},
	}

	if err := restoreChunkedVolumes(context.Background(), m, r, inspect, restoreDest, nil, false); err != nil {
		t.Fatalf("restoreChunkedVolumes() error = %v", err)
	}

	// Data must be under restoreDest/<base(source)>.
	got := filepath.Join(restoreDest, filepath.Base(phantomSource), "config.yml")
	if _, err := os.Stat(got); err != nil {
		t.Errorf("expected file at %s: %v", got, err)
	}

	// Phantom source path must NOT have been created.
	if _, err := os.Stat(phantomSource); !os.IsNotExist(err) {
		t.Errorf("phantom source %s should not exist after custom-dest restore", phantomSource)
	}
}

func TestRestoreChunkedVolumes_DefaultDestRestoresToSource(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "data.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, _, cleanup := dedup.NewTestRepoForEngine(t)
	defer cleanup()

	subID := backupTestVolume(t, r, src)

	// Target path does not exist yet — restoreChunkedVolumes must create it.
	target := filepath.Join(t.TempDir(), "restore-here")
	inspect := inspectFromJSON(t, fmt.Sprintf(`{
		"Name": "/test-container",
		"Config": {"Image": "nginx:latest"},
		"Mounts": [{"Type":"bind","Source":%q,"Destination":"/data"}]
	}`, target))

	m := dedup.Manifest{
		Files: map[string]dedup.ManifestEntry{
			containerVolPrefix + "/data": {
				Size:   100,
				Chunks: []dedup.ID{subID},
			},
		},
	}

	if err := restoreChunkedVolumes(context.Background(), m, r, inspect, "", nil, false); err != nil {
		t.Fatalf("restoreChunkedVolumes() error = %v", err)
	}

	got := filepath.Join(target, "data.txt")
	if _, err := os.Stat(got); err != nil {
		t.Errorf("expected file at %s: %v", got, err)
	}
}

func TestRestoreChunkedVolumes_CustomDestNamedVolume(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte("<h1>hi</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, _, cleanup := dedup.NewTestRepoForEngine(t)
	defer cleanup()

	subID := backupTestVolume(t, r, src)

	// Named volume: Source is /var/lib/docker/volumes/myvol/_data but the
	// restore target must be restoreDest/myvol (the volume name), not _data.
	inspect := inspectFromJSON(t, `{
		"Name": "/test-container",
		"Config": {"Image": "nginx:latest"},
		"Mounts": [{"Type":"volume","Name":"myvol","Source":"/var/lib/docker/volumes/myvol/_data","Destination":"/data"}]
	}`)

	restoreDest := t.TempDir()
	m := dedup.Manifest{
		Files: map[string]dedup.ManifestEntry{
			containerVolPrefix + "/data": {
				Size:   100,
				Chunks: []dedup.ID{subID},
			},
		},
	}

	if err := restoreChunkedVolumes(context.Background(), m, r, inspect, restoreDest, nil, false); err != nil {
		t.Fatalf("restoreChunkedVolumes() error = %v", err)
	}

	// Data under restoreDest/myvol/ (NOT restoreDest/_data/).
	got := filepath.Join(restoreDest, "myvol", "index.html")
	if _, err := os.Stat(got); err != nil {
		t.Errorf("expected file at %s: %v", got, err)
	}

	// _data directory must NOT exist.
	if _, err := os.Stat(filepath.Join(restoreDest, "_data")); err == nil {
		t.Errorf("restoreDest/_data should not exist for named-volume restore")
	}
}

func TestRestoreChunkedVolumes_SkippedVolumeNotRestored(t *testing.T) {
	r, _, cleanup := dedup.NewTestRepoForEngine(t)
	defer cleanup()

	target := filepath.Join(t.TempDir(), "should-not-exist")
	inspect := inspectFromJSON(t, fmt.Sprintf(`{
		"Name": "/test-container",
		"Config": {"Image": "nginx:latest"},
		"Mounts": [{"Type":"bind","Source":%q,"Destination":"/cache"}]
	}`, target))

	m := dedup.Manifest{
		Files: map[string]dedup.ManifestEntry{
			containerVolPrefix + "/cache": {
				Size: volumeSkippedSize,
			},
		},
	}

	if err := restoreChunkedVolumes(context.Background(), m, r, inspect, t.TempDir(), nil, false); err != nil {
		t.Fatalf("restoreChunkedVolumes() error = %v", err)
	}

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("skipped volume target %s should not exist", target)
	}
}

// TestRestoreChunkedVolumes_InvalidMountSource is a regression test for the
// path-traversal fix: a bind-mount Source outside restoreAllowedRoots must
// cause restoreChunkedVolumes to return an "invalid restore path" error before
// os.MkdirAll is ever attempted.
func TestRestoreChunkedVolumes_InvalidMountSource(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "secret.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, _, cleanup := dedup.NewTestRepoForEngine(t)
	defer cleanup()

	subID := backupTestVolume(t, r, src)

	// /dev/vault is outside restoreAllowedRoots — normalizeRestorePath must
	// reject it before os.MkdirAll is called.
	disallowedSource := "/dev/vault"
	inspect := inspectFromJSON(t, fmt.Sprintf(`{
		"Name": "/test-container",
		"Config": {"Image": "nginx:latest"},
		"Mounts": [{"Type":"bind","Source":%q,"Destination":"/data"}]
	}`, disallowedSource))

	m := dedup.Manifest{
		Files: map[string]dedup.ManifestEntry{
			containerVolPrefix + "/data": {
				Size:   100,
				Chunks: []dedup.ID{subID},
			},
		},
	}

	// restoreDest="" causes volumeRestoreTarget to return Source directly,
	// so normalizeRestorePath must reject /dev/vault.
	err := restoreChunkedVolumes(context.Background(), m, r, inspect, "", nil, false)
	if err == nil {
		t.Fatal("restoreChunkedVolumes() expected error for path outside allowed roots, got nil")
	}
	if !strings.Contains(err.Error(), "invalid restore path") {
		t.Errorf("restoreChunkedVolumes() error = %q, want it to contain \"invalid restore path\"", err.Error())
	}

	// The disallowed path must not have been created on disk.
	if _, statErr := os.Stat(disallowedSource); !os.IsNotExist(statErr) {
		t.Errorf("disallowed path %s should not have been created", disallowedSource)
	}
}

// TestRestoreChunkedVolumes_ClearsStaleFiles verifies a whole-item dedup
// container volume restore clears the target only when cleanDestination is
// set, and merges (preserves stale files) when it is not (issue #321).
func TestRestoreChunkedVolumes_ClearsStaleFiles(t *testing.T) {
	tests := []struct {
		name             string
		cleanDestination bool
		wantStalePresent bool
	}{
		{name: "clears stale when clean_destination set", cleanDestination: true, wantStalePresent: false},
		{name: "merges when clean_destination unset", cleanDestination: false, wantStalePresent: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := t.TempDir()
			if err := os.WriteFile(filepath.Join(src, "config.yml"), []byte("foo: bar\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			r, _, cleanup := dedup.NewTestRepoForEngine(t)
			defer cleanup()

			subID := backupTestVolume(t, r, src)

			// With restoreDest="" the bind-mount source IS the restore target,
			// so seed it with a stale file.
			target := t.TempDir()
			if err := os.WriteFile(filepath.Join(target, "stale.txt"), []byte("stale"), 0o644); err != nil {
				t.Fatal(err)
			}

			inspect := inspectFromJSON(t, fmt.Sprintf(`{
				"Name": "/test-container",
				"Config": {"Image": "nginx:latest"},
				"Mounts": [{"Type":"bind","Source":%q,"Destination":"/data"}]
			}`, target))

			m := dedup.Manifest{
				Files: map[string]dedup.ManifestEntry{
					containerVolPrefix + "/data": {
						Size:   100,
						Chunks: []dedup.ID{subID},
					},
				},
			}

			if err := restoreChunkedVolumes(context.Background(), m, r, inspect, "", nil, tt.cleanDestination); err != nil {
				t.Fatalf("restoreChunkedVolumes() error = %v", err)
			}

			if data, err := os.ReadFile(filepath.Join(target, "config.yml")); err != nil {
				t.Errorf("restored file missing: %v", err)
			} else if string(data) != "foo: bar\n" {
				t.Errorf("config.yml content mismatch: %q", data)
			}

			_, statErr := os.Stat(filepath.Join(target, "stale.txt"))
			if stalePresent := statErr == nil; stalePresent != tt.wantStalePresent {
				t.Errorf("stale.txt presence = %v, want %v (cleanDestination=%v, stat err=%v)",
					stalePresent, tt.wantStalePresent, tt.cleanDestination, statErr)
			}
		})
	}
}

// TestRestoreChunkedVolumes_FileMount verifies the file-mount restore branch:
// an __vol__ entry with IsFile: true is written back as a single file at the
// target path (the bind source when no custom destination), not as a directory
// tree.
func TestRestoreChunkedVolumes_FileMount(t *testing.T) {
	tests := []struct {
		name     string
		fileBody string
	}{
		{name: "restores file mount as a single file", fileBody: "#!/bin/sh\necho hi\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := filepath.Join(t.TempDir(), "hook.sh")
			if err := os.WriteFile(src, []byte(tt.fileBody), 0o755); err != nil {
				t.Fatal(err)
			}

			r, _, cleanup := dedup.NewTestRepoForEngine(t)
			defer cleanup()

			entry, err := chunkFileIntoRepo(r, src)
			if err != nil {
				t.Fatalf("chunkFileIntoRepo() error = %v", err)
			}
			entry.IsFile = true
			if err := r.Flush(); err != nil {
				t.Fatalf("Flush() error = %v", err)
			}

			// The bind-mount source IS the restore target when no custom
			// destination is set.
			target := filepath.Join(t.TempDir(), "restored-hook.sh")
			inspect := inspectFromJSON(t, fmt.Sprintf(`{
				"Name": "/test-container",
				"Config": {"Image": "nginx:latest"},
				"Mounts": [{"Type":"bind","Source":%q,"Destination":"/hook"}]
			}`, target))

			m := dedup.Manifest{
				Files: map[string]dedup.ManifestEntry{
					containerVolPrefix + "/hook": entry,
				},
			}

			if err := restoreChunkedVolumes(context.Background(), m, r, inspect, "", nil, false); err != nil {
				t.Fatalf("restoreChunkedVolumes() error = %v", err)
			}

			got, err := os.ReadFile(target)
			if err != nil {
				t.Fatalf("expected restored file at %s: %v", target, err)
			}
			if string(got) != tt.fileBody {
				t.Errorf("restored file content = %q, want %q", got, tt.fileBody)
			}
			if info, err := os.Stat(target); err != nil || info.IsDir() {
				t.Errorf("restored target should be a regular file, not a directory (stat err=%v)", err)
			}
		})
	}
}

// TestWriteChunkedRestoreSidecars verifies the chunked restore sidecar
// materialisation: a __template entry becomes a template.xml file and a
// __dbdump__ entry becomes database.sql in a classic-shaped temp dir, so the
// shared recreateAndStartContainer finds them with no special-casing.
func TestWriteChunkedRestoreSidecars(t *testing.T) {
	cases := []struct {
		name         string
		template     bool
		dump         bool
		replay       bool
		wantTemplate string
		wantDump     string
	}{
		{name: "template only", template: true, wantTemplate: "<xml/>"},
		{name: "dump only", dump: true, wantDump: "CREATE TABLE t(x);"},
		{name: "template and dump and replay marker", template: true, dump: true, replay: true, wantTemplate: "<xml/>", wantDump: "SELECT 1;"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, cleanup := dedup.NewTestRepoForEngine(t)
			defer cleanup()

			files := map[string]dedup.ManifestEntry{}
			if tc.template {
				id, err := r.Put([]byte(tc.wantTemplate))
				if err != nil {
					t.Fatal(err)
				}
				files[containerTemplateKey] = dedup.ManifestEntry{Size: int64(len(tc.wantTemplate)), Chunks: []dedup.ID{id}}
			}
			if tc.dump {
				id, err := r.Put([]byte(tc.wantDump))
				if err != nil {
					t.Fatal(err)
				}
				files[ContainerDBDumpKey] = dedup.ManifestEntry{Size: int64(len(tc.wantDump)), Chunks: []dedup.ID{id}}
			}
			if tc.replay {
				files[ContainerDBReplayKey] = dedup.ManifestEntry{}
			}
			if err := r.Flush(); err != nil {
				t.Fatal(err)
			}

			dir, cleanupDir, err := writeChunkedRestoreSidecars(r, dedup.Manifest{Files: files})
			if err != nil {
				t.Fatalf("writeChunkedRestoreSidecars() error = %v", err)
			}
			defer cleanupDir()

			if tc.template {
				got, err := os.ReadFile(filepath.Join(dir, "template.xml"))
				if err != nil {
					t.Errorf("template.xml not materialised: %v", err)
				} else if string(got) != tc.wantTemplate {
					t.Errorf("template.xml = %q, want %q", got, tc.wantTemplate)
				}
			}
			if tc.dump {
				got, err := os.ReadFile(filepath.Join(dir, DatabaseDumpFile))
				if err != nil {
					t.Errorf("database.sql not materialised: %v", err)
				} else if string(got) != tc.wantDump {
					t.Errorf("database.sql = %q, want %q", got, tc.wantDump)
				}
			}
			if tc.replay {
				if _, err := os.Stat(filepath.Join(dir, DatabaseReplayMarker)); err != nil {
					t.Errorf("replay marker not materialised: %v", err)
				}
			}
		})
	}
}

// TestRestoreChunkedFileMount_ZeroMode verifies a file-mount entry with a zero
// Mode falls back to 0644 when reconstructing the single file.
func TestRestoreChunkedFileMount_ZeroMode(t *testing.T) {
	cases := []struct {
		name string
	}{
		{name: "zero_mode_defaults_to_0644"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, cleanup := dedup.NewTestRepoForEngine(t)
			defer cleanup()

			src := filepath.Join(t.TempDir(), "hook.sh")
			if err := os.WriteFile(src, []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			entry, err := chunkFileIntoRepo(r, src)
			if err != nil {
				t.Fatalf("chunkFileIntoRepo() error = %v", err)
			}
			if err := r.Flush(); err != nil {
				t.Fatalf("Flush() error = %v", err)
			}
			entry.Mode = 0 // force the default-mode branch

			dest := filepath.Join(t.TempDir(), "out.sh")
			if err := restoreChunkedFileMount(context.Background(), r, entry, dest); err != nil {
				t.Fatalf("restoreChunkedFileMount() error = %v", err)
			}
			if info, err := os.Stat(dest); err != nil {
				t.Fatalf("stat restored file: %v", err)
			} else if info.Mode().Perm() != 0o644 {
				t.Errorf("restored mode = %o, want 644", info.Mode().Perm())
			}
			if data, err := os.ReadFile(dest); err != nil {
				t.Fatalf("read restored file: %v", err)
			} else if string(data) != "#!/bin/sh\n" {
				t.Errorf("restored content = %q, want %q", data, "#!/bin/sh\n")
			}
		})
	}
}

// TestRestoreChunkedFileMount_MissingChunk verifies a missing chunk fails with
// a "restore file mount chunk" error.
func TestRestoreChunkedFileMount_MissingChunk(t *testing.T) {
	cases := []struct {
		name string
	}{
		{name: "missing_chunk"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, cleanup := dedup.NewTestRepoForEngine(t)
			defer cleanup()

			var bogus dedup.ID
			for i := range bogus {
				bogus[i] = 0xCD
			}
			entry := dedup.ManifestEntry{Mode: 0o644, Chunks: []dedup.ID{bogus}}

			dest := filepath.Join(t.TempDir(), "out.sh")
			err := restoreChunkedFileMount(context.Background(), r, entry, dest)
			if err == nil {
				t.Fatal("restoreChunkedFileMount() expected error for missing chunk, got nil")
			}
			if !strings.Contains(err.Error(), "restore file mount chunk") {
				t.Errorf("error = %q, want it to contain %q", err.Error(), "restore file mount chunk")
			}
		})
	}
}

// TestWriteChunkedRestoreSidecars_Empty verifies the no-sidecars fast path
// returns an empty dir and a nil cleanup without touching the filesystem.
func TestWriteChunkedRestoreSidecars_Empty(t *testing.T) {
	cases := []struct {
		name string
	}{
		{name: "no_sidecars"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, cleanup := dedup.NewTestRepoForEngine(t)
			defer cleanup()

			dir, cleanupDir, err := writeChunkedRestoreSidecars(r, dedup.Manifest{Files: map[string]dedup.ManifestEntry{}})
			if err != nil {
				t.Fatalf("writeChunkedRestoreSidecars() error = %v", err)
			}
			if dir != "" {
				t.Errorf("dir = %q, want empty", dir)
			}
			if cleanupDir != nil {
				t.Errorf("cleanupDir = non-nil, want nil")
			}
		})
	}
}

// TestWriteChunkedRestoreSidecars_MissingChunk verifies a dump or template
// entry referencing a missing chunk fails the materialisation with the
// appropriate wrapped error.
func TestWriteChunkedRestoreSidecars_MissingChunk(t *testing.T) {
	tests := []struct {
		name     string
		dump     bool
		template bool
		wantText string
	}{
		{name: "dump_missing_chunk", dump: true, wantText: "database dump"},
		{name: "template_missing_chunk", template: true, wantText: "template xml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, _, cleanup := dedup.NewTestRepoForEngine(t)
			defer cleanup()

			var bogus dedup.ID
			for i := range bogus {
				bogus[i] = 0xEF
			}
			files := map[string]dedup.ManifestEntry{}
			if tc.dump {
				files[ContainerDBDumpKey] = dedup.ManifestEntry{Size: 4, Chunks: []dedup.ID{bogus}}
			}
			if tc.template {
				files[containerTemplateKey] = dedup.ManifestEntry{Size: 4, Chunks: []dedup.ID{bogus}}
			}
			if err := r.Flush(); err != nil {
				t.Fatalf("Flush() error = %v", err)
			}

			dir, cleanupDir, err := writeChunkedRestoreSidecars(r, dedup.Manifest{Files: files})
			if cleanupDir != nil {
				cleanupDir()
			}
			if err == nil {
				t.Fatal("writeChunkedRestoreSidecars() expected error for missing chunk, got nil")
			}
			if dir != "" {
				t.Errorf("dir = %q, want empty on error", dir)
			}
			if !strings.Contains(err.Error(), tc.wantText) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantText)
			}
		})
	}
}

// TestRestoreChunkedVolumes_SkipsEmptyDirAndUnmatched covers the two
// continue-skips in restoreChunkedVolumes: a malformed empty-chunks directory
// entry and an __vol__ key with no matching bind mount in the inspect data.
func TestRestoreChunkedVolumes_SkipsEmptyDirAndUnmatched(t *testing.T) {
	tests := []struct {
		name    string
		volKey  string
		entry   dedup.ManifestEntry
		mountTo string
	}{
		{
			name:    "empty_chunks_directory_entry",
			volKey:  containerVolPrefix + "/data",
			entry:   dedup.ManifestEntry{Size: 0},
			mountTo: "/data",
		},
		{
			name:    "unmatched_bind_mount",
			volKey:  containerVolPrefix + "/ghost",
			entry:   dedup.ManifestEntry{Size: 100, Chunks: []dedup.ID{{0xEE}}},
			mountTo: "/other",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, _, cleanup := dedup.NewTestRepoForEngine(t)
			defer cleanup()

			target := t.TempDir()
			inspect := inspectFromJSON(t, fmt.Sprintf(`{
				"Name": "/test-container",
				"Config": {"Image": "nginx:latest"},
				"Mounts": [{"Type":"bind","Source":%q,"Destination":%q}]
			}`, target, tc.mountTo))

			m := dedup.Manifest{
				Files: map[string]dedup.ManifestEntry{tc.volKey: tc.entry},
			}

			if err := restoreChunkedVolumes(context.Background(), m, r, inspect, t.TempDir(), nil, false); err != nil {
				t.Fatalf("restoreChunkedVolumes() error = %v", err)
			}
		})
	}
}
