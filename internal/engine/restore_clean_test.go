package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPathDepth(t *testing.T) {
	t.Parallel()

	cases := map[string]int{
		"/":                   0,
		"/mnt":                1,
		"/mnt/user":           2,
		"/mnt/user/appdata":   3,
		"/mnt/user/appdata/":  3,
		"/mnt//user//appdata": 3,
	}
	for path, want := range cases {
		if got := pathDepth(path); got != want {
			t.Errorf("pathDepth(%q) = %d, want %d", path, got, want)
		}
	}
}

// TestClearRestoreTarget is the core of issue #321: the restore wizard warns
// that existing data is overwritten, but extraction only ever wrote the
// archive's own entries, so unrelated leftovers survived and the restored tree
// was a mix of two points in time.
func TestClearRestoreTarget(t *testing.T) {
	t.Parallel()

	t.Run("removes every child and keeps the target", func(t *testing.T) {
		t.Parallel()

		target := t.TempDir()
		if err := os.WriteFile(filepath.Join(target, "stale.txt"), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(target, "sub", "deep"), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(target, "sub", "deep", "nested.txt"), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := clearRestoreTarget(target); err != nil {
			t.Fatalf("clearRestoreTarget: %v", err)
		}

		entries, err := os.ReadDir(target)
		if err != nil {
			t.Fatalf("the target itself must survive: %v", err)
		}
		if len(entries) != 0 {
			t.Fatalf("target still holds %d entries, want 0", len(entries))
		}
	})

	t.Run("unlinks a symlink child without touching its target", func(t *testing.T) {
		t.Parallel()

		outside := t.TempDir()
		keep := filepath.Join(outside, "precious.txt")
		if err := os.WriteFile(keep, []byte("do not delete"), 0o644); err != nil {
			t.Fatal(err)
		}

		target := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(target, "escape")); err != nil {
			t.Skipf("filesystem does not support symlinks: %v", err)
		}

		if err := clearRestoreTarget(target); err != nil {
			t.Fatalf("clearRestoreTarget: %v", err)
		}
		if _, err := os.Lstat(filepath.Join(target, "escape")); !os.IsNotExist(err) {
			t.Error("the symlink itself should have been removed")
		}
		if _, err := os.Stat(keep); err != nil {
			t.Errorf("clearing must never follow a symlink out of the target: %v", err)
		}
	})

	t.Run("a target that does not exist is not an error", func(t *testing.T) {
		t.Parallel()

		if err := clearRestoreTarget(filepath.Join(t.TempDir(), "never-created")); err != nil {
			t.Fatalf("clearRestoreTarget: %v", err)
		}
	})

	t.Run("refuses a target too shallow to own its contents", func(t *testing.T) {
		t.Parallel()

		err := clearRestoreTarget("/tmp")
		if !errors.Is(err, errUnsafeToClean) {
			t.Fatalf("err = %v, want errUnsafeToClean", err)
		}
	})

	t.Run("refuses a target outside the approved roots", func(t *testing.T) {
		t.Parallel()

		err := clearRestoreTarget("/usr/bin")
		if err == nil {
			t.Fatal("expected a path outside the approved roots to be refused")
		}
		if errors.Is(err, errUnsafeToClean) {
			t.Fatalf("an out-of-root path is a hard refusal, not a degradation: %v", err)
		}
	})
}

// TestCleanRestoreDestination pins when the clearing actually runs. The two
// declines matter as much as the clear: a partial restore that wiped the
// target would delete every file the user did not select.
func TestCleanRestoreDestination(t *testing.T) {
	t.Parallel()

	seed := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "stale.txt"), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	cases := []struct {
		name      string
		settings  map[string]any
		include   []string
		wantClear bool
	}{
		{name: "no setting at all leaves the target alone", settings: map[string]any{}},
		{name: "explicitly disabled leaves the target alone", settings: map[string]any{SettingCleanDestination: false}},
		{
			name:     "a partial restore is never cleared",
			settings: map[string]any{SettingCleanDestination: true},
			include:  []string{"wanted.txt"},
		},
		{
			name:      "a whole-item restore is cleared",
			settings:  map[string]any{SettingCleanDestination: true},
			wantClear: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			target := seed(t)
			item := BackupItem{Name: "test", Settings: tc.settings}
			if err := cleanRestoreDestination(item, target, tc.include); err != nil {
				t.Fatalf("cleanRestoreDestination: %v", err)
			}
			_, err := os.Stat(filepath.Join(target, "stale.txt"))
			cleared := os.IsNotExist(err)
			if cleared != tc.wantClear {
				t.Fatalf("cleared = %v, want %v", cleared, tc.wantClear)
			}
		})
	}

	t.Run("a target too shallow to clear falls back to merging", func(t *testing.T) {
		t.Parallel()

		item := BackupItem{Name: "flash", Settings: map[string]any{SettingCleanDestination: true}}
		// /tmp is an approved root but only one level deep — clearing it is
		// refused, and the restore must still go ahead.
		if err := cleanRestoreDestination(item, "/tmp", nil); err != nil {
			t.Fatalf("an unsafe target must degrade to a merge, not fail the restore: %v", err)
		}
	})
}

// TestFolderRestoreClearsDestination drives the whole handler: a file that
// predates the backup and is not in it must be gone afterwards.
func TestFolderRestoreClearsDestination(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		clean         bool
		filePaths     []string
		wantStaleGone bool
	}{
		{name: "clean restore removes the stale file", clean: true, wantStaleGone: true},
		{name: "merge restore keeps the stale file", clean: false},
		{
			name:      "partial restore keeps the stale file even when asked to clean",
			clean:     true,
			filePaths: []string{"kept.txt"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src := t.TempDir()
			if err := os.WriteFile(filepath.Join(src, "kept.txt"), []byte("backed up"), 0o644); err != nil {
				t.Fatal(err)
			}

			h := &FolderHandler{}
			destDir := t.TempDir()
			item := BackupItem{Name: "src", Type: "folder", Settings: map[string]any{"path": src}}
			if _, err := h.Backup(context.Background(), item, destDir, func(string, int, string) {}); err != nil {
				t.Fatalf("backup: %v", err)
			}

			target := t.TempDir()
			if err := os.WriteFile(filepath.Join(target, "stale.txt"), []byte("never backed up"), 0o644); err != nil {
				t.Fatal(err)
			}

			restoreItem := BackupItem{Name: "src", Type: "folder", Settings: map[string]any{
				"restore_destination":   target,
				SettingCleanDestination: tc.clean,
			}}
			if len(tc.filePaths) > 0 {
				restoreItem.Settings["restore_file_paths"] = tc.filePaths
			}
			if err := h.Restore(context.Background(), restoreItem, destDir, func(string, int, string) {}); err != nil {
				t.Fatalf("restore: %v", err)
			}

			_, err := os.Stat(filepath.Join(target, "stale.txt"))
			if os.IsNotExist(err) != tc.wantStaleGone {
				t.Errorf("stale file gone = %v, want %v", os.IsNotExist(err), tc.wantStaleGone)
			}
			if _, err := os.Stat(filepath.Join(target, "kept.txt")); err != nil {
				t.Errorf("the backed-up file must always be restored: %v", err)
			}
		})
	}
}
