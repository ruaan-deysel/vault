package unraid

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverPools_EmptyMnt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	pools := discoverPoolsIn(root)
	if len(pools) != 0 {
		t.Errorf("expected no pools, got %v", pools)
	}
}

func TestDiscoverPools_CacheFirst(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// Create dirs: nvme, cache, ssd — cache should sort first.
	for _, name := range []string{"nvme", "cache", "ssd"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}

	pools := discoverPoolsIn(root)
	if len(pools) != 3 {
		t.Fatalf("expected 3 pools, got %v", pools)
	}
	if filepath.Base(pools[0]) != "cache" {
		t.Errorf("expected cache first, got %s", pools[0])
	}
	if filepath.Base(pools[1]) != "nvme" {
		t.Errorf("expected nvme second, got %s", pools[1])
	}
	if filepath.Base(pools[2]) != "ssd" {
		t.Errorf("expected ssd third, got %s", pools[2])
	}
}

func TestDiscoverPools_ExcludesKnownDirs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// Create excluded dirs and a real pool.
	for _, name := range []string{"user", "user0", "disks", "remotes", "disk1", "disk23", "mypool"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}

	pools := discoverPoolsIn(root)
	if len(pools) != 1 {
		t.Fatalf("expected 1 pool, got %v", pools)
	}
	if filepath.Base(pools[0]) != "mypool" {
		t.Errorf("expected mypool, got %s", pools[0])
	}
}

func TestDiscoverPools_SkipsFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// Create a file (not a dir) — should be skipped.
	if err := os.WriteFile(filepath.Join(root, "notapool"), []byte("data"), 0o644); err != nil {
		t.Fatalf("writefile: %v", err)
	}
	// Create a real pool dir.
	if err := os.Mkdir(filepath.Join(root, "cache"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	pools := discoverPoolsIn(root)
	if len(pools) != 1 {
		t.Fatalf("expected 1 pool, got %v", pools)
	}
}

func TestDiscoverPools_NonexistentRoot(t *testing.T) {
	t.Parallel()
	pools := discoverPoolsIn("/nonexistent/path")
	if len(pools) != 0 {
		t.Errorf("expected no pools for nonexistent root, got %v", pools)
	}
}

func TestPreferredPool_CacheExists(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	for _, name := range []string{"nvme", "cache"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}

	pool := preferredPoolIn(root)
	expected := filepath.Join(root, "cache")
	if pool != expected {
		t.Errorf("expected %s, got %s", expected, pool)
	}
}

func TestPreferredPool_NoCacheFallback(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	if err := os.Mkdir(filepath.Join(root, "nvme"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	pool := preferredPoolIn(root)
	expected := filepath.Join(root, "nvme")
	if pool != expected {
		t.Errorf("expected %s, got %s", expected, pool)
	}
}

func TestPreferredPool_NoPools(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	pool := preferredPoolIn(root)
	if pool != "" {
		t.Errorf("expected empty string, got %s", pool)
	}
}

func TestPreferredPool_OnlyExcludedDirs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	for _, name := range []string{"user", "disks", "disk1"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}

	pool := preferredPoolIn(root)
	if pool != "" {
		t.Errorf("expected empty string, got %s", pool)
	}
}
