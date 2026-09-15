package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
	imagetypes "github.com/moby/moby/api/types/image"
	mounttypes "github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

// newTwoMountMock builds a classic-path mock whose container reports the two
// given bind mounts in exactly the order supplied, so a test can replay the
// same container with its mounts reordered.
func newTwoMountMock(t *testing.T, mounts ...containertypes.MountPoint) *mockDockerClient {
	t.Helper()
	return &mockDockerClient{
		inspectResp: client.ContainerInspectResult{
			Container: containertypes.InspectResponse{
				ID:     "abc123",
				Name:   "/test",
				Config: &containertypes.Config{Image: "nginx:latest"},
				State:  &containertypes.State{Running: false},
				Mounts: mounts,
			},
		},
		imageResp: client.ImageInspectResult{
			InspectResponse: imagetypes.InspectResponse{
				RepoDigests: []string{"nginx@sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			},
		},
	}
}

// TestBackupNamesVolumeArchivesByStableKey is the regression test for issue
// #352. A container recreate can make Docker report the same mounts in a
// different order. Under the old index-based naming the SAME volume was
// written to volume_0.tar in one run and volume_1.tar in the next, so a
// differential's archive was later merged against a different volume's base
// full. Naming by mount source makes each volume's archive name identical
// across both runs.
func TestBackupNamesVolumeArchivesByStableKey(t *testing.T) {
	t.Parallel()

	confSrc := t.TempDir()
	dataSrc := t.TempDir()
	if err := os.WriteFile(filepath.Join(confSrc, "conf.txt"), []byte("conf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataSrc, "data.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	confMount := containertypes.MountPoint{Type: mounttypes.TypeBind, Source: confSrc, Destination: "/config"}
	dataMount := containertypes.MountPoint{Type: mounttypes.TypeBind, Source: dataSrc, Destination: "/data"}

	item := BackupItem{
		Name:        "test",
		Type:        "container",
		Settings:    map[string]any{"id": "abc123"},
		Compression: CompressionNone,
	}

	// Run 1: mounts reported as (config, data).
	firstDest := t.TempDir()
	if _, err := (&ContainerHandler{cli: newTwoMountMock(t, confMount, dataMount)}).
		Backup(context.Background(), item, firstDest, noopProgress); err != nil {
		t.Fatalf("first Backup: %v", err)
	}

	// Run 2: the container was recreated and Docker now reports (data, config).
	secondDest := t.TempDir()
	if _, err := (&ContainerHandler{cli: newTwoMountMock(t, dataMount, confMount)}).
		Backup(context.Background(), item, secondDest, noopProgress); err != nil {
		t.Fatalf("second Backup: %v", err)
	}

	for _, tc := range []struct {
		name    string
		src     string
		wantFil string
	}{
		{name: "config volume", src: confSrc, wantFil: "conf.txt"},
		{name: "data volume", src: dataSrc, wantFil: "data.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := volumeArchiveBase(tc.src)
			for _, dir := range []string{firstDest, secondDest} {
				archive := filepath.Join(dir, base)
				if _, err := os.Stat(archive); err != nil {
					t.Fatalf("archive %s missing in %s: %v", base, dir, err)
				}
				// The archive named for this source must hold this source's
				// file — the union of both runs must never cross volumes.
				if names := listTarEntries(t, archive); !containsName(names, tc.wantFil) {
					t.Errorf("archive %s holds %v, want %s", base, names, tc.wantFil)
				}
			}
		})
	}

	// The legacy index names must no longer be produced at all, or a chain
	// merge across a mixed-version chain would silently union two volumes.
	for _, dir := range []string{firstDest, secondDest} {
		for i := range 2 {
			if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("volume_%d.tar", i))); err == nil {
				t.Errorf("index-named archive volume_%d.tar still written in %s", i, dir)
			}
		}
	}
}

// TestVolumeArchiveBaseIsDeterministicAndDistinct pins the naming contract the
// merge and restore paths depend on: the same source always yields the same
// name, different sources never collide, and the affixes MergeContainerChain-
// Staging and findArchive rely on are preserved.
func TestVolumeArchiveBaseIsDeterministicAndDistinct(t *testing.T) {
	t.Parallel()

	a := volumeArchiveBase("/mnt/user/appdata/plex")
	b := volumeArchiveBase("/mnt/user/appdata/plex")
	c := volumeArchiveBase("/mnt/user/appdata/sonarr")

	if a != b {
		t.Errorf("volumeArchiveBase not deterministic: %q != %q", a, b)
	}
	if a == c {
		t.Errorf("distinct sources collided on %q", a)
	}
	for _, got := range []string{a, c} {
		if len(got) < len("volume_.tar") || got[:len("volume_")] != "volume_" {
			t.Errorf("%q lost the volume_ prefix MergeContainerChainStaging scans for", got)
		}
		if filepath.Ext(got) != ".tar" {
			t.Errorf("%q lost the .tar base findArchive appends suffixes to", got)
		}
	}
}

// TestResolveVolumeArchive covers the three-step lookup that keeps restore
// points written before #352 restoring: the manifest's recorded name first,
// then the stable-key name, then the legacy index name.
func TestResolveVolumeArchive(t *testing.T) {
	t.Parallel()

	const source = "/mnt/user/appdata/plex"

	cases := []struct {
		name            string
		present         []string // files created in sourceDir
		manifestArchive string
		index           int
		want            string
		wantErr         bool
	}{
		{
			name:            "manifest name wins",
			present:         []string{volumeArchiveBase(source), "volume_3.tar"},
			manifestArchive: volumeArchiveBase(source),
			index:           3,
			want:            volumeArchiveBase(source),
		},
		{
			name:            "manifest compression suffix falls back to the merged plain base",
			present:         []string{volumeArchiveBase(source)},
			manifestArchive: volumeArchiveBase(source) + ".zst",
			index:           0,
			want:            volumeArchiveBase(source),
		},
		{
			name:    "no manifest entry resolves by stable key",
			present: []string{volumeArchiveBase(source)},
			index:   0,
			want:    volumeArchiveBase(source),
		},
		{
			name:    "legacy index-named archive still resolves",
			present: []string{"volume_2.tar"},
			index:   2,
			want:    "volume_2.tar",
		},
		{
			name:    "legacy gzip suffix still resolves",
			present: []string{"volume_1.tar.gz"},
			index:   1,
			want:    "volume_1.tar.gz",
		},
		{
			name:    "nothing present is an error",
			present: nil,
			index:   0,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range tc.present {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := resolveVolumeArchive(dir, tc.manifestArchive, source, tc.index)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveVolumeArchive() error = %v", err)
			}
			if got != filepath.Join(dir, tc.want) {
				t.Errorf("resolveVolumeArchive() = %q, want %q", got, filepath.Join(dir, tc.want))
			}
		})
	}
}

// TestFindVolumeManifestEntryCorrelatesBySource proves the manifest lookup is
// order-independent, and that the index fallback applies only to entries that
// recorded no source at all — returning another volume's entry would hand the
// restore the wrong IsFile / root-metadata.
func TestFindVolumeManifestEntryCorrelatesBySource(t *testing.T) {
	t.Parallel()

	manifest := []volumeManifestEntry{
		{Index: 0, Source: "/srv/config", Destination: "/config", BackedUp: true, RootMode: 0o750},
		{Index: 1, Source: "/srv/data", Destination: "/data", BackedUp: true, IsFile: true},
	}

	cases := []struct {
		name      string
		manifest  []volumeManifestEntry
		source    string
		index     int
		wantFound bool
		wantDest  string
	}{
		{
			name:      "matches by source when the index shifted",
			manifest:  manifest,
			source:    "/srv/data",
			index:     0, // reordered: data is now first
			wantFound: true,
			wantDest:  "/data",
		},
		{
			name:      "matches by source at its original index",
			manifest:  manifest,
			source:    "/srv/config",
			index:     0,
			wantFound: true,
			wantDest:  "/config",
		},
		{
			name:      "unknown source does not fall back to another volume",
			manifest:  manifest,
			source:    "/srv/added-later",
			index:     0,
			wantFound: false,
		},
		{
			name:      "source-less legacy entry falls back to the index",
			manifest:  []volumeManifestEntry{{Index: 1, Destination: "/legacy", BackedUp: true}},
			source:    "/srv/whatever",
			index:     1,
			wantFound: true,
			wantDest:  "/legacy",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, found := findVolumeManifestEntry(tc.manifest, tc.source, tc.index)
			if found != tc.wantFound {
				t.Fatalf("found = %v, want %v", found, tc.wantFound)
			}
			if found && got.Destination != tc.wantDest {
				t.Errorf("Destination = %q, want %q", got.Destination, tc.wantDest)
			}
		})
	}
}

// TestMergeContainerChainKeepsReorderedVolumesSeparate is the chain-level
// regression for #352. Two restore points back up the same two volumes, with
// the mounts reported in opposite order. Merging the chain must overlay each
// volume onto itself; under index naming the two volumes' contents were
// unioned into each other's archive.
func TestMergeContainerChainKeepsReorderedVolumesSeparate(t *testing.T) {
	t.Parallel()

	const confSrc = "/srv/config"
	const dataSrc = "/srv/data"

	// Build one step dir holding both volumes, each named by its stable key.
	newStep := func(t *testing.T, confFile, dataFile string) string {
		t.Helper()
		step := t.TempDir()
		for _, v := range []struct{ src, file string }{{confSrc, confFile}, {dataSrc, dataFile}} {
			tree := filepath.Join(step, "tree-"+filepath.Base(v.src))
			if err := os.MkdirAll(tree, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(tree, v.file), []byte(v.file), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := tarDirectory(context.Background(), tree, filepath.Join(step, volumeArchiveBase(v.src)), nil, CompressionNone); err != nil {
				t.Fatal(err)
			}
			if err := os.RemoveAll(tree); err != nil {
				t.Fatal(err)
			}
		}
		return step
	}

	// Full step, then a differential taken after a recreate reordered the
	// mounts. The archive names are unaffected by that reorder.
	full := newStep(t, "conf-base.txt", "data-base.txt")
	diff := newStep(t, "conf-new.txt", "data-new.txt")

	outDir := t.TempDir()
	if err := MergeContainerChainStaging(context.Background(), []string{full, diff}, outDir); err != nil {
		t.Fatalf("MergeContainerChainStaging() error = %v", err)
	}

	for _, tc := range []struct {
		name    string
		src     string
		want    []string
		notWant []string
	}{
		{name: "config volume", src: confSrc, want: []string{"conf-base.txt", "conf-new.txt"}, notWant: []string{"data-base.txt", "data-new.txt"}},
		{name: "data volume", src: dataSrc, want: []string{"data-base.txt", "data-new.txt"}, notWant: []string{"conf-base.txt", "conf-new.txt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			names := listTarEntries(t, filepath.Join(outDir, volumeArchiveBase(tc.src)))
			for _, w := range tc.want {
				if !containsName(names, w) {
					t.Errorf("merged archive missing %s: %v", w, names)
				}
			}
			for _, nw := range tc.notWant {
				if containsName(names, nw) {
					t.Errorf("merged archive leaked %s from the other volume: %v", nw, names)
				}
			}
		})
	}
}
