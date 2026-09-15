package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruaan-deysel/vault/internal/db"
)

// Dropping the run ID from the run folder (issue #319) removed the only thing
// that made the name unique. Two back-to-back manual runs of a fast job share
// a second, and the storage adapters overwrite silently — so a collision would
// destroy the earlier run's data with no error anywhere.
func TestUniqueRunPath(t *testing.T) {
	storageDir := t.TempDir()
	cfg, _ := json.Marshal(map[string]string{"path": storageDir})
	dest := db.StorageDestination{Name: "local", Type: "local", Config: string(cfg)}

	base := "nightly/2026-09-15_143000"

	if got := uniqueRunPath(dest, base); got != base {
		t.Errorf("a free path should be used as-is, got %q", got)
	}

	// Materialise the first run, then ask again.
	occupy := func(p string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(storageDir, p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(storageDir, p, "manifest.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	occupy(base)
	second := uniqueRunPath(dest, base)
	if second == base {
		t.Fatal("a taken path must not be handed out again — the earlier run would be overwritten")
	}
	if second != base+"-2" {
		t.Errorf("got %q, want %q", second, base+"-2")
	}

	occupy(second)
	third := uniqueRunPath(dest, base)
	if third != base+"-3" {
		t.Errorf("got %q, want %q", third, base+"-3")
	}

	// An unusable destination must not block the backup: the upload reports
	// the real problem with a far better message than this check could.
	broken := db.StorageDestination{Name: "broken", Type: "definitely-not-a-provider", Config: "{}"}
	if got := uniqueRunPath(broken, base); got != base {
		t.Errorf("an unreachable destination should fall through to the plain name, got %q", got)
	}
}

// The end-to-end shape: a completed run's storage path names the job and the
// time it ran, and nothing else.
func TestRunJobStoragePathHasNoRunIDPrefix(t *testing.T) {
	r, database, storageDir := setupTestRunner(t)
	dest := createLocalDest(t, database, storageDir)

	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	jobID, err := database.CreateJob(db.Job{
		Name: "nightly", StorageDestID: dest.ID, BackupTypeChain: "full", Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	settings, _ := json.Marshal(map[string]any{"path": source})
	if _, err := database.AddJobItem(db.JobItem{
		JobID: jobID, ItemType: "folder", ItemName: "src", Settings: string(settings),
	}); err != nil {
		t.Fatalf("AddJobItem: %v", err)
	}

	r.RunJob(jobID)

	rps, err := database.ListRestorePoints(jobID)
	if err != nil {
		t.Fatalf("ListRestorePoints: %v", err)
	}
	if len(rps) != 1 {
		t.Fatalf("expected 1 restore point, got %d", len(rps))
	}
	path := rps[0].StoragePath
	prefix := "nightly/"
	if !strings.HasPrefix(path, prefix) {
		t.Fatalf("storage path %q should start with the job name", path)
	}
	folder := strings.TrimPrefix(path, prefix)
	// "2026-09-15_143000" — a date, not "<id>_<date>".
	if !strings.HasPrefix(folder, "20") || strings.Count(folder, "_") != 1 {
		t.Errorf("run folder %q should be a bare timestamp, with no run ID prefix", folder)
	}
}
