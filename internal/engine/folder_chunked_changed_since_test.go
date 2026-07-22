package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruaan-deysel/vault/internal/dedup"
)

// TestFolderBackupChunked_ChangedSinceSkipsOldFiles verifies that
// BackupChunked skips files whose mtime is before changed_since.
func TestFolderBackupChunked_ChangedSinceSkipsOldFiles(t *testing.T) {
	t.Parallel()

	src := t.TempDir()

	// Create an "old" file and set its mtime to 3 hours ago.
	oldFile := filepath.Join(src, "old.txt")
	if err := os.WriteFile(oldFile, []byte("old data"), 0o600); err != nil {
		t.Fatalf("write old: %v", err)
	}
	past := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(oldFile, past, past); err != nil {
		t.Fatalf("chtimes old: %v", err)
	}

	// Create a "new" file with current mtime.
	newFile := filepath.Join(src, "new.txt")
	if err := os.WriteFile(newFile, []byte("new data"), 0o600); err != nil {
		t.Fatalf("write new: %v", err)
	}

	// Create a subdirectory with an old file.
	subDir := filepath.Join(src, "subdir")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	subOldFile := filepath.Join(subDir, "sub_old.txt")
	if err := os.WriteFile(subOldFile, []byte("sub old"), 0o600); err != nil {
		t.Fatalf("write sub old: %v", err)
	}
	if err := os.Chtimes(subOldFile, past, past); err != nil {
		t.Fatalf("chtimes sub old: %v", err)
	}

	// Open repo using the shared test helper.
	repo, _, cleanup := dedup.NewTestRepoForEngine(t)
	defer cleanup()

	// Backup with changed_since = 1 hour ago.
	changedSince := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	h := &FolderHandler{}
	manifestID, err := h.BackupChunked(context.Background(), BackupItem{
		Name: "test-folder",
		Type: "folder",
		Settings: map[string]any{
			"path":          src,
			"changed_since": changedSince,
		},
	}, repo, func(string, int, string) {})
	if err != nil {
		t.Fatalf("BackupChunked: %v", err)
	}
	if err := repo.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Retrieve manifest and verify only the new file is present.
	m, err := repo.GetManifest(manifestID)
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}

	// new.txt should be in the manifest.
	if _, ok := m.Files["new.txt"]; !ok {
		t.Error("expected new.txt in manifest")
	}

	// old.txt and subdir/sub_old.txt should NOT be in the manifest.
	if _, ok := m.Files["old.txt"]; ok {
		t.Error("expected old.txt to be skipped (mtime before changed_since)")
	}
	if _, ok := m.Files[filepath.Join("subdir", "sub_old.txt")]; ok {
		t.Error("expected subdir/sub_old.txt to be skipped (mtime before changed_since)")
	}

	// The subdir entry itself should still be recorded (directory structure preserved).
	if _, ok := m.Files["subdir"]; !ok {
		t.Error("expected subdir directory entry in manifest")
	}
}

// TestFolderBackupChunked_NoChangedSinceBacksUpAll verifies that
// omitting changed_since produces a full backup (all files in manifest).
func TestFolderBackupChunked_NoChangedSinceBacksUpAll(t *testing.T) {
	t.Parallel()

	src := t.TempDir()

	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("aaa"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	repo, _, cleanup := dedup.NewTestRepoForEngine(t)
	defer cleanup()

	h := &FolderHandler{}
	manifestID, err := h.BackupChunked(context.Background(), BackupItem{
		Name: "all-files",
		Type: "folder",
		Settings: map[string]any{
			"path": src,
		},
	}, repo, func(string, int, string) {})
	if err != nil {
		t.Fatalf("BackupChunked: %v", err)
	}
	if err := repo.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	m, err := repo.GetManifest(manifestID)
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if _, ok := m.Files["a.txt"]; !ok {
		t.Error("expected a.txt in manifest (no changed_since filter)")
	}
}

// TestFolderBackupChunked_InvalidChangedSinceFallsBackToFull verifies
// that a malformed changed_since value is silently ignored (same behaviour
// as the classic tar path).
func TestFolderBackupChunked_InvalidChangedSinceFallsBackToFull(t *testing.T) {
	t.Parallel()

	src := t.TempDir()

	if err := os.WriteFile(filepath.Join(src, "b.txt"), []byte("bbb"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	repo, _, cleanup := dedup.NewTestRepoForEngine(t)
	defer cleanup()

	h := &FolderHandler{}
	manifestID, err := h.BackupChunked(context.Background(), BackupItem{
		Name: "garbage-changed-since",
		Type: "folder",
		Settings: map[string]any{
			"path":          src,
			"changed_since": "not-rfc3339",
		},
	}, repo, func(string, int, string) {})
	if err != nil {
		t.Fatalf("BackupChunked: %v", err)
	}
	if err := repo.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	m, err := repo.GetManifest(manifestID)
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if _, ok := m.Files["b.txt"]; !ok {
		t.Error("expected b.txt in manifest (invalid changed_since should fall back to full)")
	}
}
