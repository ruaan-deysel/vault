package engine

import (
	"context"
	"fmt"

	"github.com/ruaan-deysel/vault/internal/dedup"
)

// VerifyDedupClosure re-reads every chunk reachable from top and
// re-authenticates it, mirroring the classic path's post-upload "read back +
// re-hash SHA-256" verification (job.VerifyBackup). Because chunk IDs are
// content-addressed (HMAC-BLAKE2b-256 of the plaintext) and each chunk's
// ciphertext is bound to its ID via AES-GCM additional data, a successful
// Get proves the stored bytes decrypt to exactly the content the ID names —
// so a truncated, corrupted, or swapped chunk fails here. Walks the full
// closure (container __vol__ sub-manifests and installer payloads included)
// via WalkManifestClosure, so a container restore that would touch volume
// data is verified against it too.
func VerifyDedupClosure(ctx context.Context, repo *dedup.Repo, top dedup.ID) error {
	if repo == nil {
		return fmt.Errorf("VerifyDedupClosure: nil repo")
	}
	// Manifest chunks are re-read (and GCM-authenticated) inside the walk;
	// re-read each leaf data chunk so a missing/corrupt pack fails the backup.
	_, data, err := WalkManifestClosure(repo, []dedup.ID{top})
	if err != nil {
		return err
	}
	for _, id := range data {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := repo.Get(id); err != nil {
			return fmt.Errorf("dedup verify: chunk %x: %w", id[:8], err)
		}
	}
	return nil
}
