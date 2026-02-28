package db

import (
	"path/filepath"
	"testing"
)

func setupTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestCreateAndGetStorageDestination(t *testing.T) {
	d := setupTestDB(t)
	dest := StorageDestination{Name: "local-backup", Type: "local", Config: `{"path":"/mnt/user/backups"}`}
	id, err := d.CreateStorageDestination(dest)
	if err != nil {
		t.Fatalf("Create error = %v", err)
	}
	got, err := d.GetStorageDestination(id)
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if got.Name != "local-backup" {
		t.Errorf("Name = %q, want %q", got.Name, "local-backup")
	}
}

func TestListStorageDestinations(t *testing.T) {
	d := setupTestDB(t)
	d.CreateStorageDestination(StorageDestination{Name: "a", Type: "local", Config: "{}"})
	d.CreateStorageDestination(StorageDestination{Name: "b", Type: "smb", Config: "{}"})
	dests, err := d.ListStorageDestinations()
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if len(dests) != 2 {
		t.Errorf("got %d destinations, want 2", len(dests))
	}
}

func TestDeleteStorageDestination(t *testing.T) {
	d := setupTestDB(t)
	id, _ := d.CreateStorageDestination(StorageDestination{Name: "del", Type: "local", Config: "{}"})
	if err := d.DeleteStorageDestination(id); err != nil {
		t.Fatalf("Delete error = %v", err)
	}
	_, err := d.GetStorageDestination(id)
	if err == nil {
		t.Error("Get after Delete should fail")
	}
}
