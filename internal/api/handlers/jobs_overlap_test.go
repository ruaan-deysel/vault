package handlers

import (
	"testing"

	"github.com/ruaan-deysel/vault/internal/db"
)

func TestPathsOverlap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b string
		want bool
	}{
		{"/mnt/user/appdata", "/mnt/user/appdata", true},         // equal
		{"/mnt/user", "/mnt/user/backups", true},                 // b under a
		{"/mnt/user/appdata/plex", "/mnt/user/appdata", true},    // a under b
		{"/mnt/user/appdata", "/mnt/user/media", false},          // siblings
		{"/mnt/user/appdata", "/mnt/cache/appdata", false},       // different roots
		{"/mnt/user/app", "/mnt/user/appdata", false},            // prefix but not a path parent
		{"/mnt/user/appdata/", "/mnt/user/appdata/plex/..", true}, // cleaned to equal
	}
	for _, tc := range tests {
		if got := pathsOverlap(tc.a, tc.b); got != tc.want {
			t.Errorf("pathsOverlap(%q,%q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestFolderSourceOverlap(t *testing.T) {
	t.Parallel()
	items := []db.JobItem{
		{ItemType: "folder", Settings: `{"path":"/mnt/user/documents"}`},
		{ItemType: "container", Settings: `{"path":"/mnt/user/backups"}`}, // non-folder ignored
	}

	// Destination inside the folder source → overlap.
	if _, bad := folderSourceOverlap("/mnt/user/documents/backups", items); !bad {
		t.Error("expected overlap when destination is inside the folder source")
	}
	// Unrelated destination → no overlap.
	if _, bad := folderSourceOverlap("/mnt/user/backups", items); bad {
		t.Error("unexpected overlap for a destination outside the source")
	}
	// Empty destination (remote destination) → never overlaps.
	if _, bad := folderSourceOverlap("", items); bad {
		t.Error("empty destination should never overlap")
	}
	// A container-only item's path must not count as a folder source.
	if _, bad := folderSourceOverlap("/mnt/user/backups/sub", items); bad {
		t.Error("container item path should not be treated as a folder source")
	}
}
