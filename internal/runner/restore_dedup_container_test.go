package runner

import (
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruaan-deysel/vault/internal/db"
	"github.com/ruaan-deysel/vault/internal/dedup"
	"github.com/ruaan-deysel/vault/internal/storage"
)

// TestRestoreDedupContainerDifferentialRestoresDirectly verifies that a
// differential container restore on a dedup-enabled destination takes the
// single-point dedup path (restoreSinglePoint -> restoreSinglePointChunked)
// instead of falling into restoreMergedChain -> stageRestorePointItem, which
// fails because the container data lives in the dedup chunk store, not in
// classic backup directories on storage.
//
// Regression for: differential container restore on dedup dest fails with
// "listing restore files: open <path>: no such file or directory".
func TestRestoreDedupContainerDifferentialRestoresDirectly(t *testing.T) {
	t.Parallel()

	r, database, storageDir := setupTestRunner(t)
	r.serverKey = testServerKey()
	dest := makeDedupDest(t, database, storageDir)

	// Initialise the dedup repo on disk.
	adapter, err := storage.NewAdapter(dest.Type, dest.Config)
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	_, err = dedup.InitRepo(database, adapter, dest.ID, r.serverKey)
	if err != nil {
		t.Fatalf("InitRepo: %v", err)
	}
	storage.CloseAdapter(adapter)

	// Create a job so restoreSinglePointChunked can look it up.
	jobID, err := database.CreateJob(db.Job{
		Name:          "test-containers",
		StorageDestID: dest.ID,
		Schedule:      "0 2 * * *",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	// Create a FULL restore point for the container "sonarr".
	var fullManifestBytes [32]byte
	for i := range fullManifestBytes {
		fullManifestBytes[i] = byte(0xF0 + i) // distinct from the diff manifest
	}
	fullRunID, err := database.CreateJobRun(db.JobRun{
		JobID: jobID, Status: "completed", BackupType: "full",
	})
	if err != nil {
		t.Fatalf("CreateJobRun (full): %v", err)
	}
	fullRP := db.RestorePoint{
		JobRunID:    fullRunID,
		JobID:       jobID,
		StoragePath: filepath.Join("Docker", "1_2026-07-20_020000"),
		BackupType:  "full",
		ManifestID:  fullManifestBytes[:],
	}
	fullRPID, err := database.CreateRestorePoint(fullRP)
	if err != nil {
		t.Fatalf("CreateRestorePoint (full): %v", err)
	}
	fullRP.ID = fullRPID

	// Use a real 32-byte hex manifest ID so resolveManifestID succeeds.
	var manifestBytes [32]byte
	for i := range manifestBytes {
		manifestBytes[i] = byte(i + 1)
	}
	diffManifestHex := hex.EncodeToString(manifestBytes[:])
	diffMeta := map[string]any{
		"item_manifests": map[string]any{
			"sonarr": diffManifestHex,
		},
	}
	diffMetaJSON, _ := json.Marshal(diffMeta)

	diffRunID, err := database.CreateJobRun(db.JobRun{
		JobID: jobID, Status: "completed", BackupType: "differential",
	})
	if err != nil {
		t.Fatalf("CreateJobRun (diff): %v", err)
	}
	diffRP := db.RestorePoint{
		JobRunID:             diffRunID,
		JobID:                jobID,
		StoragePath:          filepath.Join("Docker", "47_2026-07-20_130500"),
		BackupType:           "differential",
		ParentRestorePointID: fullRPID,
		Metadata:             string(diffMetaJSON),
	}
	diffRPID, err := database.CreateRestorePoint(diffRP)
	if err != nil {
		t.Fatalf("CreateRestorePoint (diff): %v", err)
	}
	diffRP.ID = diffRPID

	// RestoreItem should NOT error with "no such file or directory".
	// It will fail later (no actual dedup chunks on disk, no real container
	// handler in test) — but the key assertion is that it does NOT fail at
	// the stageRestorePointItem listing step. We accept any error that is
	// NOT the "listing restore files" / "no such file or directory" error
	// that characterises the bug.
	err = r.RestoreItem(diffRP, "sonarr", "container", "/tmp/restore-test", "")

	if err != nil && strings.Contains(err.Error(), "listing restore files") {
		t.Fatalf("restore hit the classic staging path (bug): %v", err)
	}
	// Any other error (dedup chunk not found, container handler unavailable,
	// etc.) is acceptable — the point is we took the dedup restore path,
	// not the broken classic staging path.
}
