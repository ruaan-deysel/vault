package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTarDirectory(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "file1.txt"), []byte("hello"), 0644)
	os.MkdirAll(filepath.Join(src, "subdir"), 0755)
	os.WriteFile(filepath.Join(src, "subdir", "file2.txt"), []byte("world"), 0644)

	dst := filepath.Join(t.TempDir(), "test.tar.gz")
	if err := tarDirectory(src, dst); err != nil {
		t.Fatalf("tarDirectory() error = %v", err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("tar file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Error("tar file is empty")
	}
}

func TestTarAndUntarRoundtrip(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "data.txt"), []byte("vault backup"), 0644)

	tarPath := filepath.Join(t.TempDir(), "backup.tar.gz")
	if err := tarDirectory(src, tarPath); err != nil {
		t.Fatalf("tarDirectory() error = %v", err)
	}

	restored := t.TempDir()
	if err := untarDirectory(tarPath, restored); err != nil {
		t.Fatalf("untarDirectory() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(restored, "data.txt"))
	if err != nil {
		t.Fatalf("restored file not found: %v", err)
	}
	if string(data) != "vault backup" {
		t.Errorf("data = %q, want %q", string(data), "vault backup")
	}
}
