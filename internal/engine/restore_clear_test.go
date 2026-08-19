package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClearRestoreTarget covers the clearRestoreTarget helper (issue #321).
// Table-driven: each case builds a destination, clears it, then checks the
// resulting state and error.
func TestClearRestoreTarget(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) string                  // returns the destination to clear
		check func(t *testing.T, dest string, err error) // asserts post-state + error
	}{
		{
			name: "removes contents but keeps the directory",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"a.txt", filepath.Join("sub", "b.txt")} {
					if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				return dir
			},
			check: func(t *testing.T, dest string, err error) {
				if err != nil {
					t.Fatalf("clearRestoreTarget() error = %v", err)
				}
				if info, statErr := os.Stat(dest); statErr != nil || !info.IsDir() {
					t.Fatalf("target dir should survive as a directory, stat err = %v", statErr)
				}
				entries, readErr := os.ReadDir(dest)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if len(entries) != 0 {
					t.Errorf("expected empty directory, found %d entries", len(entries))
				}
			},
		},
		{
			name: "missing directory is a no-op",
			setup: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "does-not-exist")
			},
			check: func(t *testing.T, dest string, err error) {
				if err != nil {
					t.Fatalf("clearRestoreTarget() on missing dir returned error: %v", err)
				}
				if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
					t.Errorf("missing dir should remain missing, stat err = %v", statErr)
				}
			},
		},
		{
			name: "does not follow symlinks",
			setup: func(t *testing.T) string {
				parent := t.TempDir()
				dir := filepath.Join(parent, "dest")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(parent, "outside.txt")
				if err := os.WriteFile(outside, []byte("keep me"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
					t.Skip("symlink unsupported in test fs")
				}
				if err := os.WriteFile(filepath.Join(dir, "regular.txt"), []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				return dir
			},
			check: func(t *testing.T, dest string, err error) {
				if err != nil {
					t.Fatalf("clearRestoreTarget() error = %v", err)
				}
				// The symlink's external target lives in the parent dir and
				// must survive (removed as a link, never followed).
				outside := filepath.Join(filepath.Dir(dest), "outside.txt")
				if data, readErr := os.ReadFile(outside); readErr != nil {
					t.Errorf("symlink target was followed and removed: %v", readErr)
				} else if string(data) != "keep me" {
					t.Errorf("symlink target content changed: %q", data)
				}
				entries, readErr := os.ReadDir(dest)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if len(entries) != 0 {
					t.Errorf("expected empty directory, found %d entries", len(entries))
				}
			},
		},
		{
			name: "refuses filesystem root",
			setup: func(t *testing.T) string {
				return string(filepath.Separator)
			},
			check: func(t *testing.T, dest string, err error) {
				if err == nil {
					t.Fatal("clearRestoreTarget() on filesystem root should error, got nil")
				}
				if !strings.Contains(err.Error(), "filesystem root") {
					t.Errorf("error = %q, want mention of filesystem root", err.Error())
				}
			},
		},
		{
			name: "refuses empty path",
			setup: func(t *testing.T) string {
				return ""
			},
			check: func(t *testing.T, dest string, err error) {
				if err == nil {
					t.Fatal("clearRestoreTarget() on empty path should error, got nil")
				}
				if !strings.Contains(err.Error(), "empty or current-directory") {
					t.Errorf("error = %q, want mention of empty/current-directory", err.Error())
				}
			},
		},
		{
			name: "refuses current directory",
			setup: func(t *testing.T) string {
				return "."
			},
			check: func(t *testing.T, dest string, err error) {
				if err == nil {
					t.Fatal("clearRestoreTarget() on \".\" should error, got nil")
				}
				if !strings.Contains(err.Error(), "empty or current-directory") {
					t.Errorf("error = %q, want mention of empty/current-directory", err.Error())
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := tt.setup(t)
			err := clearRestoreTarget(context.Background(), dest)
			tt.check(t, dest, err)
		})
	}
}
