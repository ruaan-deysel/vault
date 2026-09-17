package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreSecurity_UntarGuards(t *testing.T) {
	ctx := context.Background()

	// untarFile rejects suspicious destination path
	err := untarFile(ctx, "/fake.tar", "../suspicious")
	if err == nil {
		t.Fatal("untarFile accepted suspicious destination path")
	}

	// untarDirectoryFiltered rejects suspicious destination directory
	err = untarDirectoryFiltered(ctx, "/fake.tar", "../suspicious", nil)
	if err == nil {
		t.Fatal("untarDirectoryFiltered accepted suspicious destination directory")
	}

	// removeExistingNonDir rejects suspicious target path
	removeExistingNonDir("../suspicious")
	removeExistingNonDir("..\\suspicious")

	// removeExistingNonDir removes real file
	f := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(f, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	removeExistingNonDir(f)
	if _, err := os.Stat(f); err == nil {
		t.Fatal("removeExistingNonDir failed to remove real file")
	}
}

func TestRestoreSecurity_MkdirRestoredSymlink(t *testing.T) {
	dir := t.TempDir()

	// 1. Rejects suspicious path
	err := mkdirRestored("../outside", 0o755)
	if err == nil {
		t.Fatal("mkdirRestored accepted traversal path")
	}

	// 2. Rejects backslash traversal
	err = mkdirRestored("..\\outside", 0o755)
	if err == nil {
		t.Fatal("mkdirRestored accepted backslash traversal path")
	}

	// 3. Rejects existing symlink
	realDir := filepath.Join(dir, "real_dir")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkDir := filepath.Join(dir, "symlink_dir")
	if err := os.Symlink(realDir, symlinkDir); err == nil {
		err = mkdirRestored(symlinkDir, 0o755)
		if err == nil {
			t.Fatal("mkdirRestored created through symlink without error")
		}
	}
}

func TestRestoreSecurity_FolderRestoreUnsafeDestination(t *testing.T) {
	h := &FolderHandler{}
	item := BackupItem{
		Name: "test-folder",
		Settings: map[string]any{
			"restore_destination": "../suspicious",
		},
	}
	err := h.Restore(context.Background(), item, t.TempDir(), func(item string, pct int, msg string) {})
	if err == nil {
		t.Fatal("FolderHandler.Restore accepted suspicious destination")
	}
}

func TestRestoreSecurity_VMRestoreUnsafeDestination(t *testing.T) {
	h := &VMHandler{}
	item := BackupItem{
		Name: "test-vm",
		Settings: map[string]any{
			"restore_destination": "../suspicious",
		},
	}
	err := h.Restore(context.Background(), item, t.TempDir(), func(item string, pct int, msg string) {})
	if err == nil {
		t.Fatal("VMHandler.Restore accepted suspicious destination")
	}
}
