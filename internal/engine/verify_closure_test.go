package engine

import (
	"context"
	"os"
	"testing"

	"github.com/ruaan-deysel/vault/internal/dedup"
)

// TestVerifyDedupClosure_HappyPath backs up a folder into a test repo and
// verifies that VerifyDedupClosure re-reads the whole closure without error.
func TestVerifyDedupClosure_HappyPath(t *testing.T) {
	cases := []struct {
		name string
	}{
		{name: "folder_backup"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, cleanup := dedup.NewTestRepoForEngine(t)
			defer cleanup()

			fh := &FolderHandler{}
			src := t.TempDir()
			if err := osWriteFileForTest(src); err != nil {
				t.Fatal(err)
			}
			id, err := fh.BackupChunked(context.Background(), BackupItem{
				Name: "verify-folder", Type: "folder", Settings: map[string]any{"path": src},
			}, r, nil, nil)
			if err != nil {
				t.Fatalf("BackupChunked() error = %v", err)
			}
			if err := r.Flush(); err != nil {
				t.Fatalf("Flush() error = %v", err)
			}

			if err := VerifyDedupClosure(context.Background(), r, id); err != nil {
				t.Fatalf("VerifyDedupClosure() error = %v", err)
			}
		})
	}
}

// TestVerifyDedupClosure_MissingChunk drives the corruption-detection path: a
// manifest that references a chunk ID not present in the repo must fail the
// verify, mirroring the classic path's checksum-mismatch failure.
func TestVerifyDedupClosure_MissingChunk(t *testing.T) {
	cases := []struct {
		name string
	}{
		{name: "missing_chunk"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, cleanup := dedup.NewTestRepoForEngine(t)
			defer cleanup()

			var bogus dedup.ID
			for i := range bogus {
				bogus[i] = 0xAB
			}
			m := dedup.Manifest{
				Files: map[string]dedup.ManifestEntry{
					"missing.bin": {Size: 4, Chunks: []dedup.ID{bogus}},
				},
			}
			id, err := r.PutManifest("verify-missing", m)
			if err != nil {
				t.Fatalf("PutManifest() error = %v", err)
			}
			if err := r.Flush(); err != nil {
				t.Fatalf("Flush() error = %v", err)
			}

			if err := VerifyDedupClosure(context.Background(), r, id); err == nil {
				t.Fatal("VerifyDedupClosure() expected error for a missing chunk, got nil")
			}
		})
	}
}

// osWriteFileForTest is a tiny local helper: add to the same file.
func osWriteFileForTest(src string) error {
	return os.WriteFile(src+"/a.txt", []byte("hello"), 0o644)
}
