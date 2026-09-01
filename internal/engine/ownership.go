package engine

import (
	"log"
	"os"
	"sync"

	"github.com/ruaan-deysel/vault/internal/dedup"
)

// Restoring a backup has to put back what the backup captured, and on Unraid
// that includes who owns the files. Container appdata is almost always owned
// by an unprivileged account (`nobody:users`, 99:100) while Vault's daemon
// runs as root, so anything a restore creates is root-owned unless it is
// explicitly chowned back. A container that then starts as its own user
// cannot read its own config — the failure mode that made a container
// restored into a fresh directory refuse to start at all.
//
// Ownership is best-effort by design: a restore that put the bytes back but
// could not set an owner is still a restore, and the daemon may legitimately
// be running unprivileged or writing to a filesystem with no ownership model
// at all (exFAT, a mounted share). Failures are logged once rather than once
// per entry, so such a destination cannot flood the log.

var chownWarnOnce sync.Once

// applyOwner chowns path to uid/gid, following the same "negative means leave
// alone" convention as chown(2), so an unknown owner (-1, recorded by backups
// taken before ownership was captured) is a no-op rather than a reset to root.
// Symlinks are chowned themselves, never their target.
func applyOwner(path string, uid, gid int) {
	if uid < 0 && gid < 0 {
		return
	}
	if err := os.Lchown(path, uid, gid); err != nil {
		chownWarnOnce.Do(func() {
			log.Printf("engine: restore: could not set ownership of %s to %d:%d: %v "+
				"(restored files keep the daemon's own owner; further occurrences are not logged)", path, uid, gid, err)
		})
	}
}

// applyVolumeRootMeta restores the mode and ownership of a container volume's
// own root directory from what the backup manifest recorded. A zero mode means
// the backup predates this metadata: the directory is then left exactly as the
// restore created it, which is the behaviour those backups already had.
//
// It runs AFTER the volume's contents are extracted, for the same reason the
// tar extractor defers directory modes — a mode with no write bit for the
// daemon would otherwise block the extraction it precedes.
func applyVolumeRootMeta(path string, mode uint32, uid, gid int) {
	if mode == 0 {
		return
	}
	if err := os.Chmod(path, os.FileMode(mode)&os.ModePerm); err != nil {
		log.Printf("engine: restore: could not set mode on volume root %s: %v", path, err)
	}
	applyOwner(path, uid, gid)
}

// setManifestOwner records a path's numeric owner on a dedup manifest entry.
// Nothing is recorded when the platform has no notion of one, which keeps the
// manifest honest: a missing owner means "leave it alone on restore" rather
// than "set it to root".
func setManifestOwner(entry *dedup.ManifestEntry, info os.FileInfo) {
	uid, gid := fileOwner(info)
	if uid < 0 || gid < 0 {
		return
	}
	entry.UID = &uid
	entry.GID = &gid
}

// volumePointerEntry builds the synthetic `__vol__<destination>` manifest entry
// that points at a volume's own sub-manifest, carrying the mount root's mode
// and ownership. The sub-manifest describes the volume's contents but never
// the directory holding them — the walk that builds it starts at the root's
// children — so without this a volume restored into a new destination would
// get a freshly invented root directory instead of the one it was backed up
// from.
func volumePointerEntry(source string, sub dedup.ID) dedup.ManifestEntry {
	entry := dedup.ManifestEntry{Size: 0, Chunks: []dedup.ID{sub}}
	info, err := os.Stat(source)
	if err != nil {
		log.Printf("engine: chunked: could not record root metadata for volume %s: %v", source, err)
		return entry
	}
	entry.Mode = uint32(info.Mode().Perm())
	setManifestOwner(&entry, info)
	return entry
}
