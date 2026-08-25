package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruaan-deysel/vault/internal/db"
)

// TestNewItemInDifferentialJob verifies that when an existing backup job has a
// new item added to it, a subsequent incremental/differential backup treats the
// new item as a full capture (no changed_since) rather than inheriting the
// parent restore point's timestamp (issue #310).
func TestNewItemInDifferentialJob(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		chainType string
	}{
		{
			name:      "incremental job captures new item fully",
			chainType: "incremental",
		},
		{
			name:      "differential job captures new item fully",
			chainType: "differential",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, database, storageDir := setupTestRunner(t)
			dest := createLocalDest(t, database, storageDir)

			srcDir1 := t.TempDir()
			file1 := filepath.Join(srcDir1, "item1.txt")
			if err := os.WriteFile(file1, []byte("item 1 content"), 0o644); err != nil {
				t.Fatalf("write file1: %v", err)
			}

			jobID, err := database.CreateJob(db.Job{
				Name:            "test-job-" + tt.chainType,
				StorageDestID:   dest.ID,
				BackupTypeChain: tt.chainType,
				Enabled:         true,
			})
			if err != nil {
				t.Fatalf("create job: %v", err)
			}

			item1Settings, _ := json.Marshal(map[string]any{"path": srcDir1})
			if _, err := database.AddJobItem(db.JobItem{
				JobID:    jobID,
				ItemType: "folder",
				ItemName: "folder1",
				Settings: string(item1Settings),
			}); err != nil {
				t.Fatalf("add item1: %v", err)
			}

			// First run: full backup of folder1.
			r.RunJob(jobID)

			run1, err := database.GetJobRun(1)
			if err != nil {
				t.Fatalf("GetJobRun(1): %v", err)
			}
			if run1.Status != "completed" {
				t.Fatalf("run 1 status = %q, want completed", run1.Status)
			}

			rp1, err := database.GetRestorePoint(1)
			if err != nil {
				t.Fatalf("GetRestorePoint(1): %v", err)
			}
			members1, known1 := rp1.BackedUpItems()
			if !known1 {
				t.Fatal("expected known membership for run 1 restore point")
			}
			if _, ok := members1["folder1"]; !ok {
				t.Fatal("expected folder1 in run 1 restore point membership")
			}
			if _, ok := members1["folder2"]; ok {
				t.Fatal("folder2 should not be in run 1 restore point membership")
			}

			// Add second item to the job.
			srcDir2 := t.TempDir()
			file2 := filepath.Join(srcDir2, "item2.txt")
			wantContent := "item 2 content with preserved timestamp"
			if err := os.WriteFile(file2, []byte(wantContent), 0o644); err != nil {
				t.Fatalf("write file2: %v", err)
			}
			// Explicitly set mtime of file2 to the past (before run 1's restore point).
			pastTime := rp1.CreatedAt.Add(-2 * time.Hour)
			if err := os.Chtimes(file2, pastTime, pastTime); err != nil {
				t.Fatalf("chtimes file2: %v", err)
			}

			item2Settings, _ := json.Marshal(map[string]any{"path": srcDir2})
			if _, err := database.AddJobItem(db.JobItem{
				JobID:    jobID,
				ItemType: "folder",
				ItemName: "folder2",
				Settings: string(item2Settings),
			}); err != nil {
				t.Fatalf("add item2: %v", err)
			}

			// Second run: incremental/differential backup. folder1 is incremental; folder2 is new
			// and must receive a full capture.
			r.RunJob(jobID)

			run2, err := database.GetJobRun(2)
			if err != nil {
				t.Fatalf("GetJobRun(2): %v", err)
			}
			if run2.Status != "completed" {
				t.Fatalf("run 2 status = %q, want completed", run2.Status)
			}
			if run2.BackupType != tt.chainType {
				t.Fatalf("run 2 backup_type = %q, want %s", run2.BackupType, tt.chainType)
			}

			rp2, err := database.GetRestorePoint(2)
			if err != nil {
				t.Fatalf("GetRestorePoint(2): %v", err)
			}

			// Verify run 2 restore point recorded both items.
			members2, known2 := rp2.BackedUpItems()
			if !known2 {
				t.Fatal("expected known membership for run 2 restore point")
			}
			if _, ok := members2["folder1"]; !ok {
				t.Error("expected folder1 in run 2 restore point membership")
			}
			if _, ok := members2["folder2"]; !ok {
				t.Error("expected folder2 in run 2 restore point membership")
			}

			// Restore folder2 and verify its file content was captured completely despite stale mtime.
			restoreTarget := t.TempDir()
			if err := r.RestoreItem(rp2, "folder2", "folder", restoreTarget, ""); err != nil {
				t.Fatalf("RestoreItem folder2: %v", err)
			}
			restoredFile := filepath.Join(restoreTarget, "item2.txt")
			data, err := os.ReadFile(restoredFile)
			if err != nil {
				t.Fatalf("reading restored file: %v", err)
			}
			if string(data) != wantContent {
				t.Errorf("restored content = %q, want %q", string(data), wantContent)
			}
		})
	}
}
