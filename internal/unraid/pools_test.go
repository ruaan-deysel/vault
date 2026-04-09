package unraid

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverPoolsIn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(root string)
		want  []string // basenames in expected order
	}{
		{
			name:  "empty mnt returns no pools",
			setup: func(_ string) {},
			want:  nil,
		},
		{
			name: "cache sorts first",
			setup: func(root string) {
				for _, n := range []string{"nvme", "cache", "ssd"} {
					os.Mkdir(filepath.Join(root, n), 0o755)
				}
			},
			want: []string{"cache", "nvme", "ssd"},
		},
		{
			name: "excludes known dirs",
			setup: func(root string) {
				for _, n := range []string{"user", "user0", "disks", "remotes", "disk1", "disk23", "mypool"} {
					os.Mkdir(filepath.Join(root, n), 0o755)
				}
			},
			want: []string{"mypool"},
		},
		{
			name: "skips files",
			setup: func(root string) {
				os.WriteFile(filepath.Join(root, "notapool"), []byte("data"), 0o644)
				os.Mkdir(filepath.Join(root, "cache"), 0o755)
			},
			want: []string{"cache"},
		},
		{
			name:  "nonexistent root returns empty",
			setup: nil, // use a non-existent path below
			want:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var root string
			if tc.setup == nil {
				root = "/nonexistent/path"
			} else {
				root = t.TempDir()
				tc.setup(root)
			}

			pools := discoverPoolsIn(root)
			if len(tc.want) == 0 {
				if len(pools) != 0 {
					t.Errorf("expected no pools, got %v", pools)
				}
				return
			}
			if len(pools) != len(tc.want) {
				t.Fatalf("expected %d pools, got %v", len(tc.want), pools)
			}
			for i, want := range tc.want {
				if filepath.Base(pools[i]) != want {
					t.Errorf("pools[%d]: expected %s, got %s", i, want, filepath.Base(pools[i]))
				}
			}
		})
	}
}

func TestPreferredPoolIn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(root string)
		want  string // expected basename, or "" for empty result
	}{
		{
			name: "prefers cache when present",
			setup: func(root string) {
				for _, n := range []string{"nvme", "cache"} {
					os.Mkdir(filepath.Join(root, n), 0o755)
				}
			},
			want: "cache",
		},
		{
			name: "falls back to first pool when no cache",
			setup: func(root string) {
				os.Mkdir(filepath.Join(root, "nvme"), 0o755)
			},
			want: "nvme",
		},
		{
			name:  "returns empty when no pools",
			setup: func(_ string) {},
			want:  "",
		},
		{
			name: "returns empty when only excluded dirs",
			setup: func(root string) {
				for _, n := range []string{"user", "disks", "disk1"} {
					os.Mkdir(filepath.Join(root, n), 0o755)
				}
			},
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			tc.setup(root)

			pool := preferredPoolIn(root)
			if tc.want == "" {
				if pool != "" {
					t.Errorf("expected empty string, got %s", pool)
				}
				return
			}
			expected := filepath.Join(root, tc.want)
			if pool != expected {
				t.Errorf("expected %s, got %s", expected, pool)
			}
		})
	}
}
