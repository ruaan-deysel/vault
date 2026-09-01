package engine

import (
	"reflect"
	"testing"
)

func TestContainerVolumeIncludes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		selection []string
		dest      string
		want      []string
		wantVol   bool
	}{
		{
			// No picker filter at all: the pre-#275 whole-volume restore.
			name: "an empty selection restores everything", dest: "/config", wantVol: true,
		},
		{
			name:      "a file inside the volume",
			selection: []string{"/config/settings.yml"},
			dest:      "/config",
			want:      []string{"settings.yml"},
			wantVol:   true,
		},
		{
			name:      "a nested file inside the volume",
			selection: []string{"/config/logs/plex.log"},
			dest:      "/config",
			want:      []string{"logs/plex.log"},
			wantVol:   true,
		},
		{
			// Everything selected lives in a different mount, so extracting
			// this one at all is the issue #275 bug.
			name:      "nothing in this volume is selected",
			selection: []string{"/transcode/tmp.bin"},
			dest:      "/config",
			wantVol:   false,
		},
		{
			name:      "only the paths belonging to this volume are mapped",
			selection: []string{"/config/a.yml", "/transcode/tmp.bin", "/config/b.yml"},
			dest:      "/config",
			want:      []string{"a.yml", "b.yml"},
			wantVol:   true,
		},
		{
			// Picking the mount point means the whole volume, whatever else
			// the selection contains.
			name:      "the mount point itself",
			selection: []string{"/config", "/config/a.yml"},
			dest:      "/config",
			wantVol:   true,
		},
		{
			// A prefix match must respect path boundaries: /configuration is
			// a different mount from /config.
			name:      "a sibling mount with a shared prefix",
			selection: []string{"/configuration/a.yml"},
			dest:      "/config",
			wantVol:   false,
		},
		{
			name:      "trailing and duplicated slashes are normalised away",
			selection: []string{"/config//logs/", "config/b.yml"},
			dest:      "/config/",
			want:      []string{"logs", "b.yml"},
			wantVol:   true,
		},
		{
			name:      "a root mount takes every selected path",
			selection: []string{"/etc/hosts"},
			dest:      "/",
			want:      []string{"etc/hosts"},
			wantVol:   true,
		},
		{
			// A single-file bind mount: the destination is the file.
			name:      "a file mount selected by its own path",
			selection: []string{"/etc/localtime"},
			dest:      "/etc/localtime",
			wantVol:   true,
		},
		{
			name:      "empty strings in the selection are ignored",
			selection: []string{"", "   "},
			dest:      "/config",
			wantVol:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, wanted := containerVolumeIncludes(tc.selection, tc.dest)
			if wanted != tc.wantVol {
				t.Fatalf("volume wanted = %v, want %v", wanted, tc.wantVol)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("includes = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestContainerVolumePath(t *testing.T) {
	t.Parallel()

	cases := []struct{ dest, rel, want string }{
		{"/config", "settings.yml", "/config/settings.yml"},
		{"/config", "logs/plex.log", "/config/logs/plex.log"},
		{"config", "a.yml", "/config/a.yml"},
		{"/config/", "/a.yml", "/config/a.yml"},
		// A file mount: no relative part, the destination is the file.
		{"/etc/localtime", "", "/etc/localtime"},
		{"/", "etc/hosts", "/etc/hosts"},
	}
	for _, tc := range cases {
		if got := ContainerVolumePath(tc.dest, tc.rel); got != tc.want {
			t.Errorf("ContainerVolumePath(%q, %q) = %q, want %q", tc.dest, tc.rel, got, tc.want)
		}
	}
}
