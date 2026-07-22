package engine

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
	imagetypes "github.com/moby/moby/api/types/image"
	mounttypes "github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"github.com/ruaan-deysel/vault/internal/dedup"
)

// chunkedStopMock tracks ContainerStop/ContainerStart calls while
// delegating ContainerInspect and ImageInspect to canned responses.
// It satisfies dockerClient at compile time.
type chunkedStopMock struct {
	inspectResp client.ContainerInspectResult
	imageResp   client.ImageInspectResult

	stopCalled  bool
	startCalled bool
}

func (m *chunkedStopMock) ContainerList(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
	panic("unreachable")
}
func (m *chunkedStopMock) ContainerInspect(_ context.Context, _ string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	return m.inspectResp, nil
}
func (m *chunkedStopMock) ContainerCreate(_ context.Context, _ client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
	panic("unreachable")
}
func (m *chunkedStopMock) ContainerStart(_ context.Context, _ string, _ client.ContainerStartOptions) (client.ContainerStartResult, error) {
	m.startCalled = true
	return client.ContainerStartResult{}, nil
}
func (m *chunkedStopMock) ContainerStop(_ context.Context, _ string, _ client.ContainerStopOptions) (client.ContainerStopResult, error) {
	m.stopCalled = true
	return client.ContainerStopResult{}, nil
}
func (m *chunkedStopMock) ContainerRemove(_ context.Context, _ string, _ client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	panic("unreachable")
}
func (m *chunkedStopMock) ImageInspect(_ context.Context, _ string, _ ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	return m.imageResp, nil
}
func (m *chunkedStopMock) ImageSave(_ context.Context, _ []string, _ ...client.ImageSaveOption) (client.ImageSaveResult, error) {
	panic("unreachable")
}
func (m *chunkedStopMock) ImageLoad(_ context.Context, _ io.Reader, _ ...client.ImageLoadOption) (client.ImageLoadResult, error) {
	panic("unreachable")
}
func (m *chunkedStopMock) ContainerStats(_ context.Context, _ string, _ client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
	return client.ContainerStatsResult{}, nil
}
func (m *chunkedStopMock) ImagePull(_ context.Context, _ string, _ client.ImagePullOptions) (client.ImagePullResponse, error) {
	panic("unreachable")
}

var _ dockerClient = (*chunkedStopMock)(nil)

// newChunkedMock builds a chunkedStopMock wired with a single bind mount
// pointing at a temp dir with one small file, and a container in the
// specified running state.
func newChunkedMock(t *testing.T, running bool) *chunkedStopMock {
	t.Helper()
	volSrc := t.TempDir()
	if err := os.WriteFile(filepath.Join(volSrc, "data.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &chunkedStopMock{
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

// TestBackupChunked_StopsAndRestartsRunningContainer verifies that
// BackupChunked calls ContainerStop before backing up volumes and
// ContainerStart after, when the container is running and no_stop is
// not set.
func TestBackupChunked_StopsAndRestartsRunningContainer(t *testing.T) {
	mock := newChunkedMock(t, true)

	r, _, cleanup := dedup.NewTestRepoForEngine(t)
	defer cleanup()

	h := &ContainerHandler{cli: mock}
	item := BackupItem{
		Name:     "test",
		Type:     "container",
		Settings: map[string]any{"id": "abc123"},
	}

	// Must use non-nil progress because runWithRestart calls
	// progress(itemName, 92, "restarting container") when shouldRestart
	// is true.
	if _, err := h.BackupChunked(context.Background(), item, r, noopProgress); err != nil {
		t.Fatalf("BackupChunked() error = %v", err)
	}

	if !mock.stopCalled {
		t.Error("expected ContainerStop to be called for running container")
	}
	if !mock.startCalled {
		t.Error("expected ContainerStart to be called after backup")
	}
}

// TestBackupChunked_NoStopSkipsStopAndStart verifies that when no_stop
// is true, BackupChunked does NOT call ContainerStop or ContainerStart.
func TestBackupChunked_NoStopSkipsStopAndStart(t *testing.T) {
	mock := newChunkedMock(t, true)

	r, _, cleanup := dedup.NewTestRepoForEngine(t)
	defer cleanup()

	h := &ContainerHandler{cli: mock}
	item := BackupItem{
		Name: "test",
		Type: "container",
		Settings: map[string]any{
			"id":      "abc123",
			"no_stop": true,
		},
	}

	// no_stop=true means shouldRestart=false, so runWithRestart
	// never calls progress. nil is safe here.
	if _, err := h.BackupChunked(context.Background(), item, r, nil); err != nil {
		t.Fatalf("BackupChunked() error = %v", err)
	}

	if mock.stopCalled {
		t.Error("ContainerStop should NOT be called when no_stop is true")
	}
	if mock.startCalled {
		t.Error("ContainerStart should NOT be called when no_stop is true")
	}
}

// TestBackupChunked_StoppedContainerNotStoppedAgain verifies that
// BackupChunked does not call ContainerStop when the container is
// already stopped (Running: false), and does not attempt to restart it.
func TestBackupChunked_StoppedContainerNotStoppedAgain(t *testing.T) {
	mock := newChunkedMock(t, false)

	r, _, cleanup := dedup.NewTestRepoForEngine(t)
	defer cleanup()

	h := &ContainerHandler{cli: mock}
	item := BackupItem{
		Name:     "test",
		Type:     "container",
		Settings: map[string]any{"id": "abc123"},
	}

	// Container already stopped → shouldRestart=false → nil progress safe.
	if _, err := h.BackupChunked(context.Background(), item, r, nil); err != nil {
		t.Fatalf("BackupChunked() error = %v", err)
	}

	if mock.stopCalled {
		t.Error("ContainerStop should NOT be called when container is already stopped")
	}
	if mock.startCalled {
		t.Error("ContainerStart should NOT be called when container was not stopped")
	}
}

// TestBackupChunked_RestartsOnBackupError verifies that ContainerStart
// is called even when the volume backup loop returns an error, which is
// the core safety guarantee provided by runWithRestart.
func TestBackupChunked_RestartsOnBackupError(t *testing.T) {
	volSrc := t.TempDir()
	// Point the mount at a nonexistent directory to force a backup failure.
	nonexistent := filepath.Join(volSrc, "nosuchdir")

	mock := &chunkedStopMock{
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
	_, err := h.BackupChunked(context.Background(), item, r, noopProgress)
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
