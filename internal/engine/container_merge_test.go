package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestMergeContainerChainStagingOverlaysVolumes is the regression test for the
// classic-path half of issue #320. The full step's volume_0.tar holds old.txt;
// the differential step's volume_0.tar holds only new.txt. Merging must produce
// a single volume_0.tar containing BOTH, so the engine's ContainerHandler.Restore
// (which untars the merged archive once) restores the complete volume.
func TestMergeContainerChainStagingOverlaysVolumes(t *testing.T) {
	t.Parallel()

	fullDir := t.TempDir()
	diffDir := t.TempDir()

	// Full step: old.txt only.
	fullVol := filepath.Join(fullDir, "volroot")
	if err := os.MkdirAll(fullVol, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fullVol, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tarDirectory(context.Background(), fullVol, filepath.Join(fullDir, "volume_0.tar"), nil, CompressionNone); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fullDir, "config.json"), []byte(`{"full":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Differential step: new.txt only.
	diffVol := filepath.Join(diffDir, "volroot")
	if err := os.MkdirAll(diffVol, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(diffVol, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tarDirectory(context.Background(), diffVol, filepath.Join(diffDir, "volume_0.tar"), nil, CompressionNone); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(diffDir, "config.json"), []byte(`{"diff":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	if err := MergeContainerChainStaging(context.Background(), []string{fullDir, diffDir}, outDir); err != nil {
		t.Fatalf("MergeContainerChainStaging() error = %v", err)
	}

	// config.json comes from the newest step.
	cfg, err := os.ReadFile(filepath.Join(outDir, "config.json"))
	if err != nil {
		t.Fatalf("read merged config.json: %v", err)
	}
	if string(cfg) != `{"diff":true}` {
		t.Fatalf("merged config.json = %s, want the newest step's", string(cfg))
	}

	// Extract the merged volume archive and assert both files are present.
	extract := t.TempDir()
	if err := untarDirectory(context.Background(), filepath.Join(outDir, "volume_0.tar"), extract); err != nil {
		t.Fatalf("untar merged volume: %v", err)
	}
	for _, name := range []string{"old.txt", "new.txt"} {
		if _, err := os.Stat(filepath.Join(extract, name)); err != nil {
			t.Errorf("merged volume missing %s: %v", name, err)
		}
	}
}

// TestMergeCopyFilePreservesMode verifies mergeCopyFile copies the source's
// permission bits rather than defaulting to 0644 (issue #320 review feedback).
func TestMergeCopyFilePreservesMode(t *testing.T) {
	t.Parallel()

	src := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(src, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "config.json")
	if err := mergeCopyFile(src, dst); err != nil {
		t.Fatalf("mergeCopyFile: %v", err)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat dst: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("dst mode = %o, want 600", info.Mode().Perm())
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(data) != `{"a":1}` {
		t.Errorf("dst content = %q, want %q", string(data), `{"a":1}`)
	}
}
