package unraid

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// mntBase is the root directory for Unraid mount points.
const mntBase = "/mnt"

// excludedNames are known non-pool directories under /mnt/.
var excludedNames = map[string]bool{
	"user":    true,
	"user0":   true,
	"disks":   true,
	"remotes": true,
}

// arrayDiskPattern matches array disk entries like disk1, disk2, ..., disk99.
var arrayDiskPattern = regexp.MustCompile(`^disk\d+$`)

// DiscoverPools enumerates Unraid pool mount points under /mnt/ using
// exclusion-based detection. It returns a sorted slice of absolute paths
// with "/mnt/cache" sorted first (if present) for backwards compatibility.
// Returns an empty slice if /mnt/ does not exist or cannot be read.
func DiscoverPools() []string {
	return discoverPoolsIn(mntBase)
}

// discoverPoolsIn is the testable implementation that accepts a custom root.
func discoverPoolsIn(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}

	var pools []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()

		// Skip known non-pool directories.
		if excludedNames[name] {
			continue
		}

		// Skip array disks (disk1, disk2, ...).
		if arrayDiskPattern.MatchString(name) {
			continue
		}

		pools = append(pools, filepath.Join(root, name))
	}

	// Sort with "cache" first for backwards compatibility, then alphabetical.
	sort.Slice(pools, func(i, j int) bool {
		iName := filepath.Base(pools[i])
		jName := filepath.Base(pools[j])
		if iName == "cache" {
			return true
		}
		if jName == "cache" {
			return false
		}
		return iName < jName
	})

	return pools
}

// PreferredPool returns the best pool path for database/staging use.
// It prefers /mnt/cache if present, otherwise the first discovered pool.
// Returns an empty string if no pools are detected.
func PreferredPool() string {
	return preferredPoolIn(mntBase)
}

// preferredPoolIn is the testable implementation that accepts a custom root.
func preferredPoolIn(root string) string {
	// Fast path: check for the standard cache pool.
	cachePath := filepath.Join(root, "cache")
	if info, err := os.Stat(cachePath); err == nil && info.IsDir() {
		return cachePath
	}

	pools := discoverPoolsIn(root)
	if len(pools) > 0 {
		return pools[0]
	}

	return ""
}

// IsMountedPool reports whether a pool path exists and contains entries,
// indicating a mounted filesystem rather than an empty mount-point stub.
func IsMountedPool(poolPath string) bool {
	entries, err := os.ReadDir(poolPath)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if name != "." && name != ".." {
			return true
		}
	}
	return false
}
