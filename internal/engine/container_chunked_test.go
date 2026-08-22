package engine

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	containertypes "github.com/moby/moby/api/types/container"
	imagetypes "github.com/moby/moby/api/types/image"
	mounttypes "github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"github.com/ruaan-deysel/vault/internal/dedup"
)

// newRunningMock builds a mockDockerClient wired with a single bind mount
// pointing at a temp dir with one small file, and a container in the
// specified running state.
func newRunningMock(t *testing.T, running bool) *mockDockerClient {
	t.Helper()
	volSrc := t.TempDir()
	if err := os.WriteFile(filepath.Join(volSrc, "data.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &mockDockerClient{
		inspectResp: client.ContainerInspectResult{
			Container: containertypes.InspectResponse{
				ID:   "abc123",
				Name: "/test",
				Config: &containertypes.Config{
					Image: "nginx:latest",
				},
				State: &containertypes.State{Running: running},
				Mounts: []containertypes.MountPoint{
					{
						Type:        mounttypes.TypeBind,
						Source:      volSrc,
						Destination: "/data",
					},
				},
			},
		},
		imageResp: client.ImageInspectResult{
			InspectResponse: imagetypes.InspectResponse{
				RepoDigests: []string{"nginx@sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			},
		},
	}
}

// noopProgress is a no-op ProgressFunc for tests where progress callbacks
// are not under test but must be non-nil (runWithRestart calls progress
// when shouldRestart is true).
func noopProgress(_ string, _ int, _ string) {}

// TestBackupChunked_DifferentialProducesCompleteManifest is the regression
// test for issue #320 (dedup path). A differential/incremental chunked
// container backup must produce a COMPLETE volume sub-manifest — unchanged
// files must be retained — because the dedup restore path restores the
// selected manifest alone and assumes it is complete. Before the fix,
// FolderHandler.BackupChunked honoured changed_since and dropped old.txt,
// so a differential restore lost the base files.
func TestBackupChunked_DifferentialProducesCompleteManifest(t *testing.T) {
	t.Parallel()

	changedSince := time.Now().Add(-1 * time.Hour).Truncate(time.Second)

	writeOld := func(dir string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "old.txt"), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(filepath.Join(dir, "old.txt"), changedSince.Add(-time.Hour), changedSince.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	// Full backup: a single volume containing only old.txt.
	fullVol := t.TempDir()
	writeOld(fullVol)
	fullMock := &mockDockerClient{
		inspectResp: client.ContainerInspectResult{
			Container: containertypes.InspectResponse{
				ID:     "abc123",
				Name:   "/test",
				Config: &containertypes.Config{Image: "nginx:latest"},
				State:  &containertypes.State{Running: false},
				Mounts: []containertypes.MountPoint{
					{Type: mounttypes.TypeBind, Source: fullVol, Destination: "/data"},
				},
			},
		},
		imageResp: client.ImageInspectResult{
			InspectResponse: imagetypes.InspectResponse{
				RepoDigests: []string{"nginx@sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			},
		},
	}

	r, _, cleanup := dedup.NewTestRepoForEngine(t)
	defer cleanup()

	fh := &ContainerHandler{cli: fullMock}
	fullID, err := fh.BackupChunked(context.Background(), BackupItem{
		Name:     "test",
		Type:     "container",
		Settings: map[string]any{"id": "abc123"},
	}, r, nil, noopProgress)
	if err != nil {
		t.Fatalf("full BackupChunked() error = %v", err)
	}
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	parent, err := r.GetManifest(fullID)
	if err != nil {
		t.Fatalf("GetManifest(parent) error = %v", err)
	}

	// Differential backup: the same volume now holds old.txt (unchanged) plus
	// a freshly-written new.txt.
	diffVol := t.TempDir()
	writeOld(diffVol)
	if err := os.WriteFile(filepath.Join(diffVol, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	diffMock := &mockDockerClient{
		inspectResp: client.ContainerInspectResult{
			Container: containertypes.InspectResponse{
				ID:     "abc123",
				Name:   "/test",
				Config: &containertypes.Config{Image: "nginx:latest"},
				State:  &containertypes.State{Running: false},
				Mounts: []containertypes.MountPoint{
					{Type: mounttypes.TypeBind, Source: diffVol, Destination: "/data"},
				},
			},
		},
		imageResp: client.ImageInspectResult{
			InspectResponse: imagetypes.InspectResponse{
				RepoDigests: []string{"nginx@sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			},
		},
	}

	dh := &ContainerHandler{cli: diffMock}
	manifestID, err := dh.BackupChunked(context.Background(), BackupItem{
		Name: "test",
		Type: "container",
		Settings: map[string]any{
			"id":            "abc123",
			"changed_since": changedSince.UTC().Format(time.RFC3339),
		},
	}, r, &parent, noopProgress)
	if err != nil {
		t.Fatalf("differential BackupChunked() error = %v", err)
	}
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	m, err := r.GetManifest(manifestID)
	if err != nil {
		t.Fatalf("GetManifest() error = %v", err)
	}

	volEntry, ok := m.Files[containerVolPrefix+"/data"]
	if !ok {
		t.Fatalf("manifest missing %s/data entry", containerVolPrefix)
	}
	if volEntry.Size == volumeSkippedSize {
		t.Fatalf("volume was skipped (Size == volumeSkippedSize); expected a complete sub-manifest")
	}
	if len(volEntry.Chunks) != 1 {
		t.Fatalf("volume sub-manifest chunks = %d, want 1", len(volEntry.Chunks))
	}

	sub, err := r.GetManifest(volEntry.Chunks[0])
	if err != nil {
		t.Fatalf("GetManifest(sub) error = %v", err)
	}
	if _, ok := sub.Files["old.txt"]; !ok {
		t.Errorf("sub-manifest missing old.txt — unchanged files must be retained for single-point restore")
	}
	if _, ok := sub.Files["new.txt"]; !ok {
		t.Errorf("sub-manifest missing new.txt")
	}
}

// TestBackupChunked_NewVolumeChunkedFully is the regression test for the
// "new items missing" data-loss class in issue #320. A differential backup of
// a container with a newly-added volume (no matching entry in the parent
// manifest) must chunk that volume FULLY — even when the volume's files and
// directory predate changed_since — rather than recording an empty
// sub-manifest. Before the fix, changed_since was applied with a nil parent
// and every old-mtime file was silently dropped.
func TestBackupChunked_NewVolumeChunkedFully(t *testing.T) {
	t.Parallel()

	changedSince := time.Now().Add(-1 * time.Hour).Truncate(time.Second)
	writeOldFile := func(dir string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "old.txt"), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(filepath.Join(dir, "old.txt"), changedSince.Add(-time.Hour), changedSince.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	// Full backup: a container with a single volume "/data" holding old.txt.
	fullVol := t.TempDir()
	writeOldFile(fullVol)
	fullMock := &mockDockerClient{
		inspectResp: client.ContainerInspectResult{
			Container: containertypes.InspectResponse{
				ID:     "abc123",
				Name:   "/test",
				Config: &containertypes.Config{Image: "nginx:latest"},
				State:  &containertypes.State{Running: false},
				Mounts: []containertypes.MountPoint{
					{Type: mounttypes.TypeBind, Source: fullVol, Destination: "/data"},
				},
			},
		},
		imageResp: client.ImageInspectResult{
			InspectResponse: imagetypes.InspectResponse{
				RepoDigests: []string{"nginx@sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			},
		},
	}

	r, _, cleanup := dedup.NewTestRepoForEngine(t)
	defer cleanup()

	fullID, err := (&ContainerHandler{cli: fullMock}).BackupChunked(context.Background(), BackupItem{
		Name:     "test",
		Type:     "container",
		Settings: map[string]any{"id": "abc123"},
	}, r, nil, noopProgress)
	if err != nil {
		t.Fatalf("full BackupChunked() error = %v", err)
	}
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	parent, err := r.GetManifest(fullID)
	if err != nil {
		t.Fatalf("GetManifest(parent) error = %v", err)
	}

	// Differential backup: the container now exposes a DIFFERENT, newly-added
	// volume "/newdata". Its only file (and the volume dir) predate
	// changed_since, so it looks "unchanged" — but there is no parent entry to
	// carry forward from, so it must be chunked fully.
	newVol := t.TempDir()
	file := filepath.Join(newVol, "fresh_old.txt")
	if err := os.WriteFile(file, []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{file, newVol} {
		if err := os.Chtimes(p, changedSince.Add(-time.Hour), changedSince.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	diffMock := &mockDockerClient{
		inspectResp: client.ContainerInspectResult{
			Container: containertypes.InspectResponse{
				ID:     "abc123",
				Name:   "/test",
				Config: &containertypes.Config{Image: "nginx:latest"},
				State:  &containertypes.State{Running: false},
				Mounts: []containertypes.MountPoint{
					{Type: mounttypes.TypeBind, Source: newVol, Destination: "/newdata"},
				},
			},
		},
		imageResp: client.ImageInspectResult{
			InspectResponse: imagetypes.InspectResponse{
				RepoDigests: []string{"nginx@sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			},
		},
	}

	manifestID, err := (&ContainerHandler{cli: diffMock}).BackupChunked(context.Background(), BackupItem{
		Name: "test",
		Type: "container",
		Settings: map[string]any{
			"id":            "abc123",
			"changed_since": changedSince.UTC().Format(time.RFC3339),
		},
	}, r, &parent, noopProgress)
	if err != nil {
		t.Fatalf("differential BackupChunked() error = %v", err)
	}
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	m, err := r.GetManifest(manifestID)
	if err != nil {
		t.Fatalf("GetManifest() error = %v", err)
	}

	volEntry, ok := m.Files[containerVolPrefix+"/newdata"]
	if !ok {
		t.Fatalf("manifest missing %s/newdata entry", containerVolPrefix)
	}
	if volEntry.Size == volumeSkippedSize {
		t.Fatalf("new volume was skipped (Size == volumeSkippedSize); expected a complete sub-manifest")
	}
	if len(volEntry.Chunks) != 1 {
		t.Fatalf("volume sub-manifest chunks = %d, want 1", len(volEntry.Chunks))
	}

	sub, err := r.GetManifest(volEntry.Chunks[0])
	if err != nil {
		t.Fatalf("GetManifest(sub) error = %v", err)
	}
	if _, ok := sub.Files["fresh_old.txt"]; !ok {
		t.Errorf("new volume sub-manifest missing fresh_old.txt — a new volume with old-mtime files must be chunked fully")
	}
}

// TestBackupChunked_StopRestartBehaviour consolidates the stop/restart
// lifecycle tests for BackupChunked into a single table-driven test.
// Each case varies the container running state, no_stop setting, and
// optional mock error, then asserts whether ContainerStop and
// ContainerStart were called.
func TestBackupChunked_StopRestartBehaviour(t *testing.T) {
	cases := []struct {
		name          string
		running       bool
		noStop        bool
		stopErr       error
		wantStopCall  bool
		wantStartCall bool
		wantErr       bool
	}{
		{
			name:          "running container is stopped and restarted",
			running:       true,
			noStop:        false,
			wantStopCall:  true,
			wantStartCall: true,
		},
		{
			name:          "no_stop skips stop and start",
			running:       true,
			noStop:        true,
			wantStopCall:  false,
			wantStartCall: false,
		},
		{
			name:          "already stopped container is left alone",
			running:       false,
			noStop:        false,
			wantStopCall:  false,
			wantStartCall: false,
		},
		{
			name:          "ContainerStop error propagates and does not restart",
			running:       true,
			noStop:        false,
			stopErr:       errors.New("mock stop failure"),
			wantStopCall:  true,
			wantStartCall: false,
			wantErr:       true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := newRunningMock(t, tc.running)
			mock.stopErr = tc.stopErr

			r, _, cleanup := dedup.NewTestRepoForEngine(t)
			defer cleanup()

			h := &ContainerHandler{cli: mock}
			settings := map[string]any{"id": "abc123"}
			if tc.noStop {
				settings["no_stop"] = true
			}
			item := BackupItem{Name: "test", Type: "container", Settings: settings}

			// Use non-nil progress when shouldRestart may be true
			// (runWithRestart calls progress on restart).
			var progress ProgressFunc = noopProgress
			if !tc.running || tc.noStop {
				progress = nil
			}

			_, err := h.BackupChunked(context.Background(), item, r, nil, progress)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
			} else if err != nil {
				t.Fatalf("BackupChunked() unexpected error = %v", err)
			}

			if mock.stopCalled != tc.wantStopCall {
				t.Errorf("stopCalled = %v, want %v", mock.stopCalled, tc.wantStopCall)
			}
			if mock.startCalled != tc.wantStartCall {
				t.Errorf("startCalled = %v, want %v", mock.startCalled, tc.wantStartCall)
			}
		})
	}
}

// TestBackupChunked_RestartsOnBackupError verifies that ContainerStart
// is called even when the volume backup loop returns an error, which is
// the core safety guarantee provided by runWithRestart.
//
// Note: This test relies on FolderHandler.BackupChunked failing when the
// mount source directory does not exist. That coupling is intentional but
// fragile — if FolderHandler.BackupChunked ever gains a graceful skip for
// missing directories, this test would stop exercising the error path and
// silently pass without validating the restart guarantee. Consider switching
// to a mock FolderHandler if the coupling breaks.
func TestBackupChunked_RestartsOnBackupError(t *testing.T) {
	volSrc := t.TempDir()
	// Point the mount at a nonexistent directory to force a backup failure.
	nonexistent := filepath.Join(volSrc, "nosuchdir")

	mock := &mockDockerClient{
		inspectResp: client.ContainerInspectResult{
			Container: containertypes.InspectResponse{
				ID:   "fail-id",
				Name: "/fail-test",
				Config: &containertypes.Config{
					Image: "nginx:latest",
				},
				State: &containertypes.State{Running: true},
				Mounts: []containertypes.MountPoint{
					{
						Type:        mounttypes.TypeBind,
						Source:      nonexistent,
						Destination: "/data",
					},
				},
			},
		},
		imageResp: client.ImageInspectResult{
			InspectResponse: imagetypes.InspectResponse{
				RepoDigests: []string{"nginx@sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			},
		},
	}

	r, _, cleanup := dedup.NewTestRepoForEngine(t)
	defer cleanup()

	h := &ContainerHandler{cli: mock}
	item := BackupItem{
		Name:     "fail-test",
		Type:     "container",
		Settings: map[string]any{"id": "fail-id"},
	}

	// BackupChunked should fail because the mount source doesn't exist,
	// but the container must still be restarted.
	_, err := h.BackupChunked(context.Background(), item, r, nil, noopProgress)
	if err == nil {
		t.Fatal("expected BackupChunked to fail for nonexistent mount source")
	}

	if !mock.stopCalled {
		t.Error("expected ContainerStop to be called before backup attempt")
	}
	if !mock.startCalled {
		t.Error("expected ContainerStart to be called even though backup failed (runWithRestart guarantee)")
	}
}

// newChunkedMockForChangedSince builds a mockDockerClient with a single
// bind mount whose file tree (file and containing dir) is pinned to
// volMtime via os.Chtimes, letting changed_since tests control whether
// pathChangedSince sees the volume as changed.
func newChunkedMockForChangedSince(t *testing.T, volMtime time.Time) *mockDockerClient {
	t.Helper()
	volSrc := t.TempDir()
	file := filepath.Join(volSrc, "data.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{file, volSrc} {
		if err := os.Chtimes(p, volMtime, volMtime); err != nil {
			t.Fatal(err)
		}
	}
	return &mockDockerClient{
		inspectResp: client.ContainerInspectResult{
			Container: containertypes.InspectResponse{
				ID:     "abc123",
				Name:   "/test",
				Config: &containertypes.Config{Image: "nginx:latest"},
				State:  &containertypes.State{Running: true},
				Mounts: []containertypes.MountPoint{
					{Type: mounttypes.TypeBind, Source: volSrc, Destination: "/data"},
				},
			},
		},
		imageResp: client.ImageInspectResult{
			InspectResponse: imagetypes.InspectResponse{
				RepoDigests: []string{"nginx@sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			},
		},
	}
}

// TestBackupChunked_DifferentialStopBehaviour consolidates the differential
// pre-check tests for BackupChunked into a single table-driven test.
// Each case varies whether the volume's files are older or newer than the
// changed_since reference, then asserts whether ContainerStop and
// ContainerStart were called. The unchanged case additionally verifies the
// manifest records a complete sub-manifest for the unchanged volume (rather
// than a volumeSkippedSize sentinel).
func TestBackupChunked_DifferentialStopBehaviour(t *testing.T) {
	reference := time.Now().Add(-1 * time.Hour)

	cases := []struct {
		name                     string
		volMtime                 time.Time // mtime applied to the volume's file and dir
		wantStopCall             bool
		wantStartCall            bool
		checkCompleteSubManifest bool // when true, verify the volume was NOT skipped (a complete sub-manifest was recorded)
	}{
		{
			name:                     "no_changes_skips_stop",
			volMtime:                 time.Now().Add(-2 * time.Hour),
			wantStopCall:             false,
			wantStartCall:            false,
			checkCompleteSubManifest: true,
		},
		{
			name:          "volume_changed_stops_and_restarts",
			volMtime:      time.Now(), // fresh — after the reference
			wantStopCall:  true,
			wantStartCall: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := newChunkedMockForChangedSince(t, tc.volMtime)

			r, _, cleanup := dedup.NewTestRepoForEngine(t)
			defer cleanup()

			h := &ContainerHandler{cli: mock}
			item := BackupItem{
				Name: "test",
				Type: "container",
				Settings: map[string]any{
					"id":            "abc123",
					"changed_since": reference.UTC().Format(time.RFC3339),
				},
			}

			manifestID, err := h.BackupChunked(context.Background(), item, r, nil, noopProgress)
			if err != nil {
				t.Fatalf("BackupChunked() error = %v", err)
			}
			if mock.stopCalled != tc.wantStopCall {
				t.Errorf("stopCalled = %v, want %v", mock.stopCalled, tc.wantStopCall)
			}
			if mock.startCalled != tc.wantStartCall {
				t.Errorf("startCalled = %v, want %v", mock.startCalled, tc.wantStartCall)
			}

			if tc.checkCompleteSubManifest {
				if err := r.Flush(); err != nil {
					t.Fatalf("Flush() error = %v", err)
				}
				m, err := r.GetManifest(manifestID)
				if err != nil {
					t.Fatalf("GetManifest() error = %v", err)
				}
				entry, ok := m.Files[containerVolPrefix+"/data"]
				if !ok {
					t.Fatalf("manifest missing %s/data entry", containerVolPrefix)
				}
				if entry.Size == volumeSkippedSize {
					t.Errorf("volume entry was skipped (Size == volumeSkippedSize); expected a complete sub-manifest")
				}
				if len(entry.Chunks) != 1 {
					t.Errorf("volume sub-manifest chunks = %d, want 1", len(entry.Chunks))
				}
			}
		})
	}
}

// TestBackupChunked_AllMountsExcluded guards the dedup path against the same
// silent data-loss trap the classic path already refuses: every backup-eligible
// mount excluded, producing a "successful" restore point that holds no volume
// data and cannot reconstruct the container. Before the guard, BackupChunked
// recorded each excluded mount as skipped and committed the manifest anyway.
//
// The container must NOT be stopped for a run that cannot produce anything.
func TestBackupChunked_AllMountsExcluded(t *testing.T) {
	mock := newRunningMock(t, true)

	r, _, cleanup := dedup.NewTestRepoForEngine(t)
	defer cleanup()

	h := &ContainerHandler{cli: mock}
	item := BackupItem{
		Name: "all-excluded",
		Type: "container",
		Settings: map[string]any{
			"id": "abc123",
			// newRunningMock's only eligible mount is /data.
			"exclude_paths": []any{"/data"},
		},
	}

	_, err := h.BackupChunked(context.Background(), item, r, nil, noopProgress)
	if err == nil {
		t.Fatal("BackupChunked succeeded with every eligible mount excluded — " +
			"that commits a restore point holding no volume data")
	}
	if !strings.Contains(err.Error(), "excluded") {
		t.Errorf("error should name exclusion as the cause, got: %v", err)
	}
	if mock.stopCalled {
		t.Error("container was stopped for a run that cannot produce a backup — " +
			"the guard must run before the stop")
	}
}

// TestBackupChunked_CapturesTemplate is the parity regression test for the
// dedup path: the classic Backup saves the Unraid template XML as a
// template.xml sidecar, but BackupChunked never captured it, so a dedup
// restore could not recreate the Unraid Docker Manager template. The chunked
// path now records it under the __template manifest key.
func TestBackupChunked_CapturesTemplate(t *testing.T) {
	// Redirect pluginsDir (package var) so templateDir() resolves under a
	// tempdir. Do NOT call t.Parallel — pluginsDir is global.
	orig := pluginsDir
	pluginsDir = t.TempDir()
	t.Cleanup(func() { pluginsDir = orig })

	templateBody := []byte(`<?xml version="1.0"?><Container version="2"/>`)
	templatePath := filepath.Join(templateDir(), "my-test.xml")

	cases := []struct {
		name string
		body []byte
	}{
		{name: "template xml is captured", body: templateBody},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.MkdirAll(filepath.Dir(templatePath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(templatePath, tc.body, 0o600); err != nil {
				t.Fatal(err)
			}

			mock := newRunningMock(t, false) // Name "/test", bind mount at a temp dir
			r, _, cleanup := dedup.NewTestRepoForEngine(t)
			defer cleanup()

			id, err := (&ContainerHandler{cli: mock}).BackupChunked(context.Background(), BackupItem{
				Name: "test", Type: "container", Settings: map[string]any{"id": "abc123"},
			}, r, nil, noopProgress)
			if err != nil {
				t.Fatalf("BackupChunked() error = %v", err)
			}
			if err := r.Flush(); err != nil {
				t.Fatalf("Flush() error = %v", err)
			}

			m, err := r.GetManifest(id)
			if err != nil {
				t.Fatalf("GetManifest() error = %v", err)
			}
			entry, ok := m.Files[containerTemplateKey]
			if !ok {
				t.Fatalf("manifest missing %s entry", containerTemplateKey)
			}
			if len(entry.Chunks) != 1 {
				t.Fatalf("template chunks = %d, want 1", len(entry.Chunks))
			}
			got, err := r.Get(entry.Chunks[0])
			if err != nil {
				t.Fatalf("Get(template chunk) error = %v", err)
			}
			if string(got) != string(tc.body) {
				t.Errorf("template body = %q, want %q", got, tc.body)
			}
		})
	}
}

// TestBackupChunked_TemplateCarryForward verifies the differential template
// gate: an unchanged template is carried forward from the parent manifest,
// while a changed template is re-chunked.
func TestBackupChunked_TemplateCarryForward(t *testing.T) {
	orig := pluginsDir
	pluginsDir = t.TempDir()
	t.Cleanup(func() { pluginsDir = orig })

	changedSince := time.Now().Add(-1 * time.Hour).Truncate(time.Second)
	templatePath := filepath.Join(templateDir(), "my-test.xml")
	writeTemplate := func(body string, mtime time.Time) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(templatePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(templatePath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(templatePath, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name              string
		body              string
		mtime             time.Time
		parentHasTemplate bool
		wantCarry         bool
		wantBody          string
	}{
		{name: "unchanged template is carried forward", body: "OLD", mtime: changedSince.Add(-time.Hour), parentHasTemplate: true, wantCarry: true, wantBody: "OLD"},
		{name: "changed template is re-chunked", body: "NEW", mtime: time.Now(), parentHasTemplate: true, wantCarry: false, wantBody: "NEW"},
		{name: "unchanged template with no parent entry is re-chunked", body: "OLD", mtime: changedSince.Add(-time.Hour), parentHasTemplate: false, wantCarry: false, wantBody: "OLD"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Full backup first: template OLD with an old mtime (unless the
			// case models a parent with no __template entry, in which case no
			// template is written so the parent manifest omits it).
			if tc.parentHasTemplate {
				writeTemplate("OLD", changedSince.Add(-time.Hour))
			} else if err := os.Remove(templatePath); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			r, _, cleanup := dedup.NewTestRepoForEngine(t)
			defer cleanup()

			fullID, err := (&ContainerHandler{cli: newRunningMock(t, false)}).BackupChunked(context.Background(), BackupItem{
				Name: "test", Type: "container", Settings: map[string]any{"id": "abc123"},
			}, r, nil, noopProgress)
			if err != nil {
				t.Fatalf("full BackupChunked() error = %v", err)
			}
			if err := r.Flush(); err != nil {
				t.Fatalf("Flush() error = %v", err)
			}
			parent, err := r.GetManifest(fullID)
			if err != nil {
				t.Fatalf("GetManifest(parent) error = %v", err)
			}

			// Differential backup with the template now in tc state.
			writeTemplate(tc.body, tc.mtime)
			diffID, err := (&ContainerHandler{cli: newRunningMock(t, false)}).BackupChunked(context.Background(), BackupItem{
				Name: "test", Type: "container",
				Settings: map[string]any{"id": "abc123", "changed_since": changedSince.UTC().Format(time.RFC3339)},
			}, r, &parent, noopProgress)
			if err != nil {
				t.Fatalf("differential BackupChunked() error = %v", err)
			}
			if err := r.Flush(); err != nil {
				t.Fatalf("Flush() error = %v", err)
			}
			m, err := r.GetManifest(diffID)
			if err != nil {
				t.Fatalf("GetManifest(diff) error = %v", err)
			}
			entry, ok := m.Files[containerTemplateKey]
			if !ok {
				t.Fatalf("manifest missing %s entry", containerTemplateKey)
			}
			if tc.wantCarry {
				parentEntry := parent.Files[containerTemplateKey]
				if len(entry.Chunks) != len(parentEntry.Chunks) || (len(entry.Chunks) > 0 && entry.Chunks[0] != parentEntry.Chunks[0]) {
					t.Errorf("expected carried-forward template chunk %x, got %x", parentEntry.Chunks, entry.Chunks)
				}
			}
			got, err := r.Get(entry.Chunks[0])
			if err != nil {
				t.Fatalf("Get(template chunk) error = %v", err)
			}
			if string(got) != tc.wantBody {
				t.Errorf("template body = %q, want %q", got, tc.wantBody)
			}
		})
	}
}

// TestBackupChunked_FileBindMount is the parity regression test for file-based
// bind mounts: the classic path detects a regular-file mount source via
// os.Lstat and archives it as a single file, while BackupChunked handed every
// volume to FolderHandler.BackupChunked → os.OpenRoot, which fails on a
// non-directory. The chunked path now chunks the single file and records it
// with IsFile: true so restore writes it back as one file.
func TestBackupChunked_FileBindMount(t *testing.T) {
	fileSrc := filepath.Join(t.TempDir(), "hook.sh")
	const fileBody = "#!/bin/sh\necho hi\n"
	if err := os.WriteFile(fileSrc, []byte(fileBody), 0o755); err != nil {
		t.Fatal(err)
	}
	mock := &mockDockerClient{
		inspectResp: client.ContainerInspectResult{
			Container: containertypes.InspectResponse{
				ID:     "abc123",
				Name:   "/file-mount",
				Config: &containertypes.Config{Image: "nginx:latest"},
				State:  &containertypes.State{Running: false},
				Mounts: []containertypes.MountPoint{
					{Type: mounttypes.TypeBind, Source: fileSrc, Destination: "/hook"},
				},
			},
		},
		imageResp: client.ImageInspectResult{
			InspectResponse: imagetypes.InspectResponse{
				RepoDigests: []string{"nginx@sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			},
		},
	}

	r, _, cleanup := dedup.NewTestRepoForEngine(t)
	defer cleanup()

	id, err := (&ContainerHandler{cli: mock}).BackupChunked(context.Background(), BackupItem{
		Name: "file-mount", Type: "container", Settings: map[string]any{"id": "abc123"},
	}, r, nil, noopProgress)
	if err != nil {
		t.Fatalf("BackupChunked() error = %v", err)
	}
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	m, err := r.GetManifest(id)
	if err != nil {
		t.Fatalf("GetManifest() error = %v", err)
	}
	entry, ok := m.Files[containerVolPrefix+"/hook"]
	if !ok {
		t.Fatalf("manifest missing %s/hook entry", containerVolPrefix)
	}
	if entry.Size == volumeSkippedSize {
		t.Fatalf("file mount was skipped (Size == volumeSkippedSize)")
	}
	if !entry.IsFile {
		t.Fatalf("entry.IsFile = false, want true for a file-based bind mount")
	}
	if len(entry.Chunks) == 0 {
		t.Fatalf("file mount entry has no chunks")
	}
	got, err := r.Get(entry.Chunks[0])
	if err != nil {
		t.Fatalf("Get(file chunk) error = %v", err)
	}
	if string(got) != fileBody {
		t.Errorf("file mount content = %q, want %q", got, fileBody)
	}
}

// TestBackupChunked_SkipsSocketMount verifies the chunked path skips a
// socket/pipe/device mount with a skipped-entry sentinel instead of aborting,
// mirroring the classic path's os.Lstat inode-type skip.
func TestBackupChunked_SkipsSocketMount(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "docker.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	mock := &mockDockerClient{
		inspectResp: client.ContainerInspectResult{
			Container: containertypes.InspectResponse{
				ID:     "abc123",
				Name:   "/sock-mount",
				Config: &containertypes.Config{Image: "nginx:latest"},
				State:  &containertypes.State{Running: false},
				Mounts: []containertypes.MountPoint{
					{Type: mounttypes.TypeBind, Source: sockPath, Destination: "/run/docker.sock"},
				},
			},
		},
		imageResp: client.ImageInspectResult{
			InspectResponse: imagetypes.InspectResponse{
				RepoDigests: []string{"nginx@sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			},
		},
	}

	r, _, cleanup := dedup.NewTestRepoForEngine(t)
	defer cleanup()

	id, err := (&ContainerHandler{cli: mock}).BackupChunked(context.Background(), BackupItem{
		Name: "sock-mount", Type: "container", Settings: map[string]any{"id": "abc123"},
	}, r, nil, noopProgress)
	if err != nil {
		t.Fatalf("BackupChunked() error = %v", err)
	}
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	m, err := r.GetManifest(id)
	if err != nil {
		t.Fatalf("GetManifest() error = %v", err)
	}
	entry, ok := m.Files[containerVolPrefix+"/run/docker.sock"]
	if !ok {
		t.Fatalf("manifest missing %s/run/docker.sock entry", containerVolPrefix)
	}
	if entry.Size != volumeSkippedSize {
		t.Fatalf("socket mount entry Size = %d, want %d (skipped sentinel)", entry.Size, volumeSkippedSize)
	}
}
