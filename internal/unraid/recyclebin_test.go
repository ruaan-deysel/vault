package unraid

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecycleBinInstalled(t *testing.T) {
	orig := recycleBinPlgPath
	t.Cleanup(func() { recycleBinPlgPath = orig })

	// Missing installer → not installed.
	recycleBinPlgPath = filepath.Join(t.TempDir(), "recycle.bin.plg")
	if RecycleBinInstalled() {
		t.Error("expected not installed when .plg is absent")
	}

	// Present installer → installed.
	if err := os.WriteFile(recycleBinPlgPath, []byte("<PLUGIN/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !RecycleBinInstalled() {
		t.Error("expected installed when .plg is present")
	}
}
