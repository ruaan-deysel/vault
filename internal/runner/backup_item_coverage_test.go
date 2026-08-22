package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ruaan-deysel/vault/internal/db"
	"github.com/ruaan-deysel/vault/internal/engine"
)

// TestBackupItem_UnknownType drives the early error branch when
// stageItemLocally is asked to handle a type it doesn't know.
func TestBackupItem_UnknownType(t *testing.T) {
	t.Parallel()
	r, _ := newTestRunner(t)

	cfg, _ := json.Marshal(map[string]string{"path": filepath.Join(t.TempDir(), "store")})
	dest := db.StorageDestination{Type: "local", Config: string(cfg)}

	_, _, err := r.backupItem(
		context.Background(),
		engine.BackupItem{Name: "x", Type: "garbage-type"},
		dest,
		"sp",
		false,
		"",
		"none",
		1,
		nil,
	)
	if err == nil {
		t.Fatal("expected unknown-type error")
	}
}

// TestBackupItem_VMHandlerError exercises the stageItemLocally error
// branch on non-Linux hosts where NewVMHandler always errors.
func TestBackupItem_VMHandlerError(t *testing.T) {
	t.Parallel()
	r, _ := newTestRunner(t)

	cfg, _ := json.Marshal(map[string]string{"path": filepath.Join(t.TempDir(), "store")})
	dest := db.StorageDestination{Type: "local", Config: string(cfg)}

	_, _, err := r.backupItem(
		context.Background(),
		engine.BackupItem{Name: "x", Type: "vm"},
		dest,
		"sp",
		false,
		"",
		"none",
		1,
		nil,
	)
	if err == nil {
		t.Fatal("expected VMHandler init error on non-Linux")
	}
}

// TestBackupItem_FolderHappyPath drives the full happy path:
// stageItemLocally creates a tmpDir, NewFolderHandler succeeds, the
// engine writes a tar archive, uploadStagedFiles streams it to storage.
// Requires a real source directory and a destination directory.
func TestBackupItem_FolderHappyPath(t *testing.T) {
	t.Parallel()
	r, _ := newTestRunner(t)

	// Source dir with a file to back up.
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("setup source: %v", err)
	}

	// Local storage destination.
	storageDir := filepath.Join(t.TempDir(), "store")
	cfg, _ := json.Marshal(map[string]string{"path": storageDir})
	dest := db.StorageDestination{Type: "local", Config: string(cfg)}

	item := engine.BackupItem{
		Name: "src-folder",
		Type: "folder",
		Settings: map[string]any{
			"path": srcDir,
		},
	}

	_, checksums, err := r.backupItem(context.Background(), item, dest, "rp-test", false, "", "none", 1, nil)
	if err != nil {
		t.Fatalf("backupItem: %v", err)
	}
	// At least one upload should have been recorded.
	if len(checksums) == 0 {
		t.Errorf("expected at least one checksum, got 0")
	}
}

// TestBackupItem_ChunkedVerify verifies job.VerifyBackup is honoured on dedup
// destinations instead of being silently dropped: with verify=true the chunked
// path records a non-empty checksum (so the restore point is marked verified),
// and with verify=false it records none.
func TestBackupItem_ChunkedVerify(t *testing.T) {
	cases := []struct {
		name          string
		verify        bool
		wantChecksums bool
	}{
		{name: "verify enabled records a checksum", verify: true, wantChecksums: true},
		{name: "verify disabled records none", verify: false, wantChecksums: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, database, storageDir := setupTestRunner(t)
			r.serverKey = testServerKey()
			dest := makeDedupDest(t, database, storageDir)

			srcDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("hello"), 0o644); err != nil {
				t.Fatal(err)
			}
			item := engine.BackupItem{Name: "folder1", Type: "folder", Settings: map[string]any{"path": srcDir}}

			_, checksums, err := r.backupItem(context.Background(), item, dest, "rp", tc.verify, "", "none", 1, nil)
			if err != nil {
				t.Fatalf("backupItem: %v", err)
			}
			if tc.wantChecksums && len(checksums) == 0 {
				t.Errorf("verify=true should record a checksum, got none")
			}
			if !tc.wantChecksums && len(checksums) != 0 {
				t.Errorf("verify=false should record no checksums, got %d", len(checksums))
			}
		})
	}
}
