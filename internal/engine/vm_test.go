package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVMBackupResultTypes(t *testing.T) {
	result := &BackupResult{
		ItemName: "test-vm",
		Success:  true,
		Files: []BackupFile{
			{Name: "domain.xml", Size: 4096},
			{Name: "vdisk0.qcow2", Size: 10737418240},
		},
	}
	if !result.Success {
		t.Error("expected success")
	}
	if len(result.Files) != 2 {
		t.Errorf("expected 2 files, got %d", len(result.Files))
	}
}

func TestCopyFile(t *testing.T) {
	src := filepath.Join(t.TempDir(), "source.bin")
	dst := filepath.Join(t.TempDir(), "dest.bin")

	data := []byte("test vm disk data")
	os.WriteFile(src, data, 0644)

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile() error = %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("got %q, want %q", got, data)
	}
}

func TestCopyFileWithProgress(t *testing.T) {
	src := filepath.Join(t.TempDir(), "source.bin")
	dst := filepath.Join(t.TempDir(), "dest.bin")

	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i % 256)
	}
	os.WriteFile(src, data, 0644)

	var progressCalled bool
	err := copyFileWithProgress(src, dst, func(bytesCopied int64) {
		progressCalled = true
	})
	if err != nil {
		t.Fatalf("copyFileWithProgress() error = %v", err)
	}
	if !progressCalled {
		t.Error("expected progress callback to be called")
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if len(got) != len(data) {
		t.Errorf("got %d bytes, want %d", len(got), len(data))
	}
}

func TestCopyFileSourceNotFound(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "dest.bin")
	err := copyFile("/nonexistent/file.bin", dst)
	if err == nil {
		t.Fatal("expected error for missing source file")
	}
}

func TestNewVMHandlerPlatform(t *testing.T) {
	// On non-Linux platforms, NewVMHandler should return an error.
	// On Linux without libvirt, it will also return an error.
	// Either way, we just verify the function exists and returns.
	_, err := NewVMHandler()
	if err == nil {
		t.Skip("libvirt available, skipping platform check")
	}
	t.Logf("NewVMHandler() returned expected error: %v", err)
}
