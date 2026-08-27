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
