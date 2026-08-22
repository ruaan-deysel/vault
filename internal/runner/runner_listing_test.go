package runner

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/ruaan-deysel/vault/internal/crypto"
	"github.com/ruaan-deysel/vault/internal/db"
	"github.com/ruaan-deysel/vault/internal/storage"
	"github.com/ruaan-deysel/vault/internal/ws"
)

// TestLoadParentVolumeListingPaths verifies the runner reads the parent
// restore point's volumes.json manifest and per-volume effective-listing
// sidecars into a mount-source -> path-list map for classic container
// differentials (issue #320).
func TestLoadParentVolumeListingPaths(t *testing.T) {
	t.Parallel()

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	const (
		sourcePath = "/mnt/cache/appdata/foo"
		itemPrefix = "chain-test/1_full/my-item"
	)

	cases := []struct {
		name  string
		files map[string]string
		want  map[string][]string
	}{
		{
			name: "manifest and listing resolve to per-volume paths",
			files: map[string]string{
				itemPrefix + "/volumes.json":              `[{"index":0,"source":"` + sourcePath + `","destination":"/data","backed_up":true,"archive":"volume_0.tar"}]`,
				itemPrefix + "/volume_0.tar.listing.json": `{"version":1,"archive":"volume_0.tar","files":[{"path":"old.txt"},{"path":"sub/nested.txt"}]}`,
			},
			want: map[string][]string{
				sourcePath: {"old.txt", "sub/nested.txt"},
			},
		},
		{
			name:  "missing manifest returns nil",
			files: map[string]string{},
			want:  nil,
		},
		{
			name: "manifest without a listing returns nil",
			files: map[string]string{
				itemPrefix + "/volumes.json": `[{"index":0,"source":"` + sourcePath + `","destination":"/data","backed_up":true,"archive":"volume_0.tar"}]`,
			},
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			storageRoot := t.TempDir()
			storageConfig := fmt.Sprintf(`{"path":%q}`, storageRoot)
			adapter, err := storage.NewAdapter("local", storageConfig)
			if err != nil {
				t.Fatalf("NewAdapter: %v", err)
			}
			t.Cleanup(func() { storage.CloseAdapter(adapter) })

			writeStorageFiles(t, adapter, tc.files)

			dest := db.StorageDestination{Type: "local", Config: storageConfig}
			parentRP := db.RestorePoint{StoragePath: "chain-test/1_full"}

			r := New(database, ws.NewHub(), nil)
			got := r.loadParentVolumeListingPaths(&parentRP, dest, "my-item", "")
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("loadParentVolumeListingPaths() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// writeEncryptedStorageFile writes content to adapter at path as an
// age-encrypted blob — the on-disk form the encrypted upload path produces
// (the storage name carries a .age suffix, which the caller encodes in path).
func writeEncryptedStorageFile(t *testing.T, adapter storage.Adapter, path, content, passphrase string) {
	t.Helper()
	enc, err := crypto.EncryptReader(passphrase, strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	encBytes, err := io.ReadAll(enc)
	_ = enc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Write(path, bytes.NewReader(encBytes)); err != nil {
		t.Fatalf("adapter.Write(%s): %v", path, err)
	}
}

// TestLoadParentVolumeListingPaths_Encrypted verifies that a classic container
// differential's per-volume effective-listing sidecars are encrypted at rest
// (stored as .age) and correctly decrypted back on read. The listing carries
// the full file set used to detect stale-mtime new files (issue #320), so it
// must not leak plaintext paths when the job has encryption enabled.
func TestLoadParentVolumeListingPaths_Encrypted(t *testing.T) {
	t.Parallel()

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	const (
		sourcePath = "/mnt/cache/appdata/foo"
		itemPrefix = "chain-test/1_full/my-item"
		passphrase = "hunter2"
	)

	cases := []struct {
		name       string
		passphrase string
		want       map[string][]string
	}{
		{
			name:       "correct passphrase decrypts the encrypted listing",
			passphrase: passphrase,
			want:       map[string][]string{sourcePath: {"old.txt", "sub/nested.txt"}},
		},
		{
			name:       "wrong passphrase returns nil",
			passphrase: "wrong-passphrase",
			want:       nil,
		},
		{
			name:       "no passphrase returns nil",
			passphrase: "",
			want:       nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			storageRoot := t.TempDir()
			storageConfig := fmt.Sprintf(`{"path":%q}`, storageRoot)
			adapter, err := storage.NewAdapter("local", storageConfig)
			if err != nil {
				t.Fatalf("NewAdapter: %v", err)
			}
			t.Cleanup(func() { storage.CloseAdapter(adapter) })

			// Write both the volumes.json manifest and the volume listing
			// sidecar encrypted with `passphrase`, under their .age names —
			// the exact on-disk form the encrypted upload path produces.
			writeEncryptedStorageFile(t, adapter, itemPrefix+"/volumes.json.age",
				`[{"index":0,"source":"`+sourcePath+`","destination":"/data","backed_up":true,"archive":"volume_0.tar"}]`, passphrase)
			writeEncryptedStorageFile(t, adapter, itemPrefix+"/volume_0.tar.listing.json.age",
				`{"version":1,"archive":"volume_0.tar","files":[{"path":"old.txt"},{"path":"sub/nested.txt"}]}`, passphrase)

			dest := db.StorageDestination{Type: "local", Config: storageConfig}
			parentRP := db.RestorePoint{StoragePath: "chain-test/1_full"}

			r := New(database, ws.NewHub(), nil)
			got := r.loadParentVolumeListingPaths(&parentRP, dest, "my-item", tc.passphrase)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("loadParentVolumeListingPaths() = %#v, want %#v", got, tc.want)
			}
		})
	}
}
