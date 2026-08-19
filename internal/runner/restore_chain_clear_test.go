package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/ruaan-deysel/vault/internal/db"
)

// TestRestoreItemChainClearsOnlyBaseStep pins the runner's clear-once
// decision (issue #321, Gap A): restoreItemChain passes cleanDestination=i==0
// into restoreSinglePoint, so an incremental classic folder chain must clear
// the target only before the base step and let the increment overlay replay
// on top. The engine-level TestFolderChainRestoreClearsOnlyBaseStep drives
// FolderHandler.Restore directly and therefore does not exercise the i==0
// computation; this test drives restoreItemChain end-to-end so a regression
// that changes `i == 0` to a constant fails:
//   - constant true  → the increment step also clears, wiping base.txt
//   - constant false → the base step never clears, leaving stale.txt
func TestRestoreItemChainClearsOnlyBaseStep(t *testing.T) {
	r, database, storageDir := setupTestRunner(t)
	dest := createLocalDest(t, database, storageDir)

	jobID, err := database.CreateJob(db.Job{
		Name:            "chain-clear",
		BackupTypeChain: "incremental",
		Compression:     "none",
		StorageDestID:   dest.ID,
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	// Base full restore point + increment, linked via parent_restore_point_id.
	runID1, err := database.CreateJobRun(db.JobRun{JobID: jobID, Status: "completed", BackupType: "full"})
	if err != nil {
		t.Fatalf("CreateJobRun(base): %v", err)
	}
	baseID, err := database.CreateRestorePoint(db.RestorePoint{
		JobRunID: runID1, JobID: jobID, BackupType: "full",
		StoragePath: "chain-clear/full", Metadata: "{}",
	})
	if err != nil {
		t.Fatalf("CreateRestorePoint(base): %v", err)
	}
	runID2, err := database.CreateJobRun(db.JobRun{JobID: jobID, Status: "completed", BackupType: "incremental"})
	if err != nil {
		t.Fatalf("CreateJobRun(inc): %v", err)
	}
	incID, err := database.CreateRestorePoint(db.RestorePoint{
		JobRunID: runID2, JobID: jobID, BackupType: "incremental",
		StoragePath: "chain-clear/inc", Metadata: "{}", ParentRestorePointID: baseID,
	})
	if err != nil {
		t.Fatalf("CreateRestorePoint(inc): %v", err)
	}
	incRP, err := database.GetRestorePoint(incID)
	if err != nil {
		t.Fatalf("GetRestorePoint(inc): %v", err)
	}

	// Lay down the archived trees in local storage: the base carries
	// base.txt, the increment carries inc.txt.
	writeChainArchive(t, storageDir, "chain-clear/full/my-folder", map[string]string{"base.txt": "base"})
	writeChainArchive(t, storageDir, "chain-clear/inc/my-folder", map[string]string{"inc.txt": "inc"})

	// Restore target already holds a stale file that only the base clear
	// must remove.
	targetDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(targetDir, "stale.txt"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	err = r.restoreItemChain(context.Background(), incRP, "my-folder", "folder", targetDir, "", nil,
		restoreProgressReporter{ItemName: "my-folder", ItemType: "folder"})
	if err != nil {
		t.Fatalf("restoreItemChain: %v", err)
	}

	checks := []struct {
		name        string
		rel         string
		wantContent string
		wantExists  bool
	}{
		{name: "base step cleared stale file", rel: "stale.txt", wantExists: false},
		{name: "increment step preserved base file", rel: "base.txt", wantContent: "base", wantExists: true},
		{name: "increment step applied its file", rel: "inc.txt", wantContent: "inc", wantExists: true},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(targetDir, tc.rel)
			data, readErr := os.ReadFile(p)
			if tc.wantExists {
				if readErr != nil {
					t.Fatalf("expected %s to exist, got err %v", tc.rel, readErr)
				}
				if string(data) != tc.wantContent {
					t.Errorf("%s = %q, want %q", tc.rel, string(data), tc.wantContent)
				}
				return
			}
			if !os.IsNotExist(readErr) {
				t.Errorf("%s should have been cleared, got err %v (data %q)", tc.rel, readErr, string(data))
			}
		})
	}
}

// writeChainArchive writes a single data.tar holding the given regular files
// into storageDir at itemDir/data.tar, mirroring a classic folder backup's
// on-disk layout (plain tar, no compression).
func writeChainArchive(t *testing.T, storageDir, itemDir string, files map[string]string) {
	t.Helper()
	full := filepath.Join(storageDir, itemDir)
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		content := files[n]
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(full, "data.tar"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
