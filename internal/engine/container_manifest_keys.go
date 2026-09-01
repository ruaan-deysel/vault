package engine

import (
	"strings"

	"github.com/ruaan-deysel/vault/internal/dedup"
)

// The container dedup manifest's Files map holds only synthetic keys — see the
// key contract documented above BackupChunked in container.go. Backup, restore,
// and the restore-point browsing API all need to agree on what each key means;
// these exported helpers are the single authority so a reader can never invent
// its own rules and drift from what the engine wrote.

// IsSyntheticContainerKey reports whether a container manifest key holds engine
// metadata rather than restorable file content (__inspect, __image_meta).
//
// Such entries must never surface in a file picker: they are internal JSON
// blobs, and their sizes describe the blob rather than any restored file.
func IsSyntheticContainerKey(key string) bool {
	return key == containerInspectKey || key == containerImageMetaKey
}

// ContainerVolumeDest returns the container-internal destination path a
// __vol__<destination> key refers to, and whether the key is a volume entry.
func ContainerVolumeDest(key string) (string, bool) {
	if !strings.HasPrefix(key, containerVolPrefix) {
		return "", false
	}
	return strings.TrimPrefix(key, containerVolPrefix), true
}

// IsSkippedVolumeEntry reports whether a __vol__ entry records a volume that
// was deliberately not backed up (shouldSkipVolume returned true, or the
// volume was excluded from the job).
//
// Backup preserves such entries for diagnostics with the volumeSkippedSize
// sentinel and no chunks, so any consumer that presents volumes to a user must
// filter them out — otherwise an excluded mount appears restorable and its
// sentinel size renders as "-1 B" (issue #333).
func IsSkippedVolumeEntry(e dedup.ManifestEntry) bool {
	return e.Size == volumeSkippedSize || len(e.Chunks) == 0
}
