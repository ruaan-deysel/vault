package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

func TestSFTPStatVFSToCapacityHappyPath(t *testing.T) {
	t.Parallel()
	st := &sftp.StatVFS{
		Bsize:  4096,
		Frsize: 4096,
		Blocks: 100 << 20, // 100 Mi blocks * 4 KiB = 400 GiB
		Bavail: 25 << 20,  // 25 Mi blocks * 4 KiB = 100 GiB free
	}
	now := time.Now().UTC()
	cap, err := sftpStatVFSToCapacity(st, now)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if cap.Source != "sftp-statvfs" {
		t.Errorf("source = %q", cap.Source)
	}
	if want := int64(100<<20) * 4096; cap.TotalBytes != want {
		t.Errorf("total = %d, want %d", cap.TotalBytes, want)
	}
	if want := int64(25<<20) * 4096; cap.FreeBytes != want {
		t.Errorf("free = %d, want %d", cap.FreeBytes, want)
	}
	if cap.UsedBytes != cap.TotalBytes-cap.FreeBytes {
		t.Errorf("used = %d, expected total-free = %d", cap.UsedBytes, cap.TotalBytes-cap.FreeBytes)
	}
	if !cap.ProbedAt.Equal(now) {
		t.Errorf("ProbedAt = %v, want %v", cap.ProbedAt, now)
	}
}

func TestSFTPStatVFSToCapacityZeroFrsize(t *testing.T) {
	t.Parallel()
	_, err := sftpStatVFSToCapacity(&sftp.StatVFS{Frsize: 0, Blocks: 1000}, time.Now().UTC())
	if err == nil {
		t.Error("expected error for zero Frsize")
	}
}

func TestSFTPStatVFSToCapacityNilInput(t *testing.T) {
	t.Parallel()
	_, err := sftpStatVFSToCapacity(nil, time.Now().UTC())
	if err == nil {
		t.Error("expected error for nil StatVFS")
	}
}

// TestSFTPGetCapacityContextCancelled verifies the early ctx.Err()
// check shorts out before the connect() call. Uses an unreachable
// host (port 1 is reserved); if the ctx check works, no dial happens.
func TestSFTPGetCapacityContextCancelled(t *testing.T) {
	t.Parallel()
	a, err := NewSFTPAdapter(SFTPConfig{Host: "127.0.0.1", Port: 1, User: "x", Password: "y"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = a.GetCapacity(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	// The early ctx.Err() path returns context.Canceled directly (no wrap).
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestIsSFTPNotFound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"fs.ErrNotExist", fs.ErrNotExist, true},
		{"os.ErrNotExist", os.ErrNotExist, true},
		{"wrapped fs.ErrNotExist", fmt.Errorf("read: %w", fs.ErrNotExist), true},
		{"wrapped os.ErrNotExist", &os.PathError{Op: "open", Path: "/missing", Err: os.ErrNotExist}, true},
		{"sftp.ErrSSHFxNoSuchFile fxerr", sftp.ErrSSHFxNoSuchFile, true},
		{"sftp.StatusError with Code 2", &sftp.StatusError{Code: 2}, true},
		{"other sftp status error", sftp.ErrSSHFxPermissionDenied, false},
		{"other generic error", errors.New("connection failed"), false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isSFTPNotFound(tt.err); got != tt.want {
				t.Errorf("isSFTPNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

type fakeStorageFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	isDir   bool
}

func (f fakeStorageFileInfo) Name() string       { return f.name }
func (f fakeStorageFileInfo) Size() int64        { return f.size }
func (f fakeStorageFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeStorageFileInfo) ModTime() time.Time { return f.modTime }
func (f fakeStorageFileInfo) IsDir() bool        { return f.isDir }
func (f fakeStorageFileInfo) Sys() any           { return nil }

func TestSFTPAdapterList(t *testing.T) {
	t.Parallel()

	t.Run("not found error normalized to fs.ErrNotExist", func(t *testing.T) {
		t.Parallel()
		a := &SFTPAdapter{
			config: SFTPConfig{BasePath: "/backups"},
			readDir: func(fullPath string) ([]os.FileInfo, error) {
				return nil, os.ErrNotExist
			},
		}
		files, err := a.List("nonexistent")
		if files != nil {
			t.Errorf("expected nil files, got %v", files)
		}
		if !IsNotExist(err) {
			t.Errorf("expected IsNotExist(err)=true, got err=%v", err)
		}
	})

	t.Run("sftp status error normalized to fs.ErrNotExist", func(t *testing.T) {
		t.Parallel()
		a := &SFTPAdapter{
			config: SFTPConfig{BasePath: "/backups"},
			readDir: func(fullPath string) ([]os.FileInfo, error) {
				return nil, sftp.ErrSSHFxNoSuchFile
			},
		}
		files, err := a.List("missing")
		if files != nil {
			t.Errorf("expected nil files, got %v", files)
		}
		if !IsNotExist(err) {
			t.Errorf("expected IsNotExist(err)=true, got err=%v", err)
		}
	})

	t.Run("generic error preserved as hard error", func(t *testing.T) {
		t.Parallel()
		a := &SFTPAdapter{
			config: SFTPConfig{BasePath: "/backups"},
			readDir: func(fullPath string) ([]os.FileInfo, error) {
				return nil, errors.New("i/o failure")
			},
		}
		files, err := a.List("corrupt")
		if files != nil {
			t.Errorf("expected nil files, got %v", files)
		}
		if err == nil || IsNotExist(err) {
			t.Errorf("expected non-IsNotExist error, got err=%v", err)
		}
	})

	t.Run("successful listing", func(t *testing.T) {
		t.Parallel()
		now := time.Now().UTC()
		a := &SFTPAdapter{
			config: SFTPConfig{BasePath: "/backups"},
			readDir: func(fullPath string) ([]os.FileInfo, error) {
				return []os.FileInfo{
					fakeStorageFileInfo{name: "file.tar", size: 1024, modTime: now, isDir: false},
					fakeStorageFileInfo{name: "subdir", size: 0, modTime: now, isDir: true},
				}, nil
			},
		}
		files, err := a.List("my-item")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 2 {
			t.Fatalf("got %d files, want 2", len(files))
		}
		if files[0].Path != "my-item/file.tar" || files[0].Size != 1024 || files[0].IsDir {
			t.Errorf("files[0] mismatch: %+v", files[0])
		}
		if files[1].Path != "my-item/subdir" || !files[1].IsDir {
			t.Errorf("files[1] mismatch: %+v", files[1])
		}
	})
}
