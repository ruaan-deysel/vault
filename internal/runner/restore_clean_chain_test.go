package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ruaan-deysel/vault/internal/db"
)

// TestChainRestoreClearsDestinationOnce is the chain half of issue #321. The
// destination must be cleared before the chain starts replaying — but exactly
// once. Clearing before every step would delete the base full's files as soon
// as the increment replayed on top, leaving only whatever the last step
// happened to contain.
func TestChainRestoreClearsDestinationOnce(t *testing.T) {
	storageDir := t.TempDir()
	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "from-full.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, d := newTestRunner(t)
	destCfg, _ := json.Marshal(map[string]string{"path": storageDir})
	destID, err := d.CreateStorageDestination(db.StorageDestination{
		Name: "clean-local", Type: "local", Config: string(destCfg),
	})
	if err != nil {
		t.Fatal(err)
	}
	jobID, err := d.CreateJob(db.Job{
		Name: "clean-job", StorageDestID: destID,
		BackupTypeChain: "incremental", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	itemSettings, _ := json.Marshal(map[string]any{"path": sourceDir})
	if _, err := d.AddJobItem(db.JobItem{
		JobID: jobID, ItemType: "folder", ItemName: "src", Settings: string(itemSettings),
	}); err != nil {
		t.Fatal(err)
	}

	r.RunJob(jobID) // base full

	if err := os.WriteFile(filepath.Join(sourceDir, "from-increment.txt"), []byte("later"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.RunJob(jobID) // incremental

	rps, err := d.ListRestorePoints(jobID)
	if err != nil {
		t.Fatal(err)
	}
	var inc db.RestorePoint
	for _, rp := range rps {
		if rp.BackupType == "incremental" {
			inc = rp
		}
	}
	if inc.ID == 0 {
		t.Fatalf("no incremental restore point; got %+v", rps)
	}

	// A file that was never in any backup — exactly what issue #321 reported
	// surviving a restore that promised to overwrite.
	restoreDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(restoreDir, "pre-existing.txt"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := r.RestoreItem(inc, "src", "folder", restoreDir, ""); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if _, err := os.Stat(filepath.Join(restoreDir, "pre-existing.txt")); !os.IsNotExist(err) {
		t.Error("a file that was never backed up survived a clean restore")
	}
	for _, want := range []string{"from-full.txt", "from-increment.txt"} {
		if _, err := os.Stat(filepath.Join(restoreDir, want)); err != nil {
			t.Errorf("%s is missing — the clearing ran on more than the first chain step: %v", want, err)
		}
	}
}
