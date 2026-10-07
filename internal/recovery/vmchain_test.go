package recovery

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeClassicRun lays out one classic run the way the runner stores it:
// <job>/<run>/manifest.json plus one folder per item holding its files.
func writeClassicRun(t *testing.T, root, job, run, backupType, created string, item Item, files map[string]string) {
	t.Helper()
	dir := filepath.Join(root, job, run)
	if err := os.MkdirAll(filepath.Join(dir, item.Name), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest, _ := json.Marshal(map[string]any{
		"version": 1, "job_name": job, "backup_type": backupType, "encryption": "none",
		"created_at": created, "items": []map[string]string{{"name": item.Name, "type": item.Type}},
	})
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, item.Name, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestExtractVMChainKeepsEveryRun checks an item with no tree archive (a VM)
// in an incremental chain is recovered one folder per run, so a later run's
// delta disk never replaces the full backup's disk.
func TestExtractVMChainKeepsEveryRun(t *testing.T) {
	root := t.TempDir()
	vm := Item{Name: "Home Assistant", Type: "vm"}
	writeClassicRun(t, root, "VMs", "2026-10-01_023000", "full", "2026-10-01T02:30:00Z", vm,
		map[string]string{"vdisk1.img": "full disk", "domain.xml": "<domain/>"})
	writeClassicRun(t, root, "VMs", "2026-10-02_023000", "incremental", "2026-10-02T02:30:00Z", vm,
		map[string]string{"vdisk1.img": "delta", "domain.xml": "<domain v2/>"})

	s := openFixture(t, root)
	p, err := s.FindPoint("VMs/latest")
	if err != nil {
		t.Fatal(err)
	}
	var notes []string
	rep, err := s.Extract(context.Background(), p, ExtractOptions{Dest: t.TempDir(), Progress: func(l string) { notes = append(notes, l) }})
	if err != nil || rep.Failed() {
		t.Fatalf("Extract: %+v, %v", rep, err)
	}
	dir := rep.Items[0].Dir
	for run, want := range map[string]string{"2026-10-01_023000": "full disk", "2026-10-02_023000": "delta"} {
		got, err := os.ReadFile(filepath.Join(dir, run, "vdisk1.img"))
		if err != nil || string(got) != want {
			t.Errorf("%s/vdisk1.img = %q, %v; want %q", run, got, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "vdisk1.img")); err == nil {
		t.Error("vdisk1.img was also written at the item root, where runs would overwrite each other")
	}
	if len(notes) == 0 || notes[len(notes)-1] == "" {
		t.Error("no note telling the user the runs must be combined")
	}

	// A single full run stays flat.
	full, _ := s.FindPoint("VMs/2026-10-01_023000")
	rep, err = s.Extract(context.Background(), full, ExtractOptions{Dest: t.TempDir()})
	if err != nil || rep.Failed() {
		t.Fatalf("Extract full: %+v, %v", rep, err)
	}
	if got, err := os.ReadFile(filepath.Join(rep.Items[0].Dir, "vdisk1.img")); err != nil || string(got) != "full disk" {
		t.Fatalf("full run vdisk1.img = %q, %v", got, err)
	}
}
