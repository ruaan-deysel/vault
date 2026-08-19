package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClearRestoreTargetRemovesContentsKeepsDir verifies the helper empties a
// directory's contents but leaves the directory itself in place (issue #321:
// a whole-item restore must land on a clean directory, not merge with stale
// pre-existing files).
func TestClearRestoreTargetRemovesContentsKeepsDir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b.txt"), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := clearRestoreTarget(context.Background(), dir); err != nil {
		t.Fatalf("clearRestoreTarget() error = %v", err)
	}

	// The directory itself must survive.
	if info, err := os.Stat(dir); err != nil {
		t.Fatalf("target dir removed: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("target path is no longer a directory")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty dir, found %d entries", len(entries))
	}
}

// TestClearRestoreTargetMissingDirNoop verifies a missing destination is a
// no-op so the caller's MkdirAll still creates it.
func TestClearRestoreTargetMissingDirNoop(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if err := clearRestoreTarget(context.Background(), missing); err != nil {
		t.Fatalf("clearRestoreTarget() on missing dir returned error: %v", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("missing dir should remain missing after no-op clear, stat err = %v", err)
	}
}

// TestClearRestoreTargetDoesNotFollowSymlinks verifies a symlinked entry is
// removed as a link (never traversed), so the link's external target survives.
func TestClearRestoreTargetDoesNotFollowSymlinks(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Skip("symlink unsupported in test fs")
	}
	if err := os.WriteFile(filepath.Join(dir, "regular.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := clearRestoreTarget(context.Background(), dir); err != nil {
		t.Fatalf("clearRestoreTarget() error = %v", err)
	}

	// The external target must still exist and hold its content.
	if data, err := os.ReadFile(outside); err != nil {
		t.Errorf("symlink target was followed and removed: %v", err)
	} else if string(data) != "keep me" {
		t.Errorf("symlink target content changed: %q", data)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty dir, found %d entries", len(entries))
	}
}

// TestClearRestoreTargetRefusesRoot verifies the filesystem-root guard.
func TestClearRestoreTargetRefusesRoot(t *testing.T) {
	err := clearRestoreTarget(context.Background(), string(filepath.Separator))
	if err == nil {
		t.Fatal("clearRestoreTarget() on filesystem root should error, got nil")
	}
	if !strings.Contains(err.Error(), "filesystem root") {
		t.Errorf("error = %q, want it to mention filesystem root", err.Error())
	}
}
