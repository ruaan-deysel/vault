package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// clearRestoreTarget removes every entry inside destDir without removing
// destDir itself, so a whole-item restore lands on a clean directory instead
// of merging the archived tree with whatever already lived there (issue #321).
// It refuses the filesystem root (defense-in-depth — the caller's
// normalizeRestorePath already rejects it upstream). A missing destDir is a
// no-op so the caller's MkdirAll still creates it. Symlinked entries are
// removed as links, never followed (os.RemoveAll does not traverse a
// symlink's target).
func clearRestoreTarget(ctx context.Context, destDir string) error {
	clean := filepath.Clean(destDir)
	// filepath.Clean("") and filepath.Clean(".") both yield "." — a bare
	// current-directory path is just as destructive as the root (it would
	// os.RemoveAll every entry in the process working directory), so reject
	// it explicitly alongside the root check.
	if clean == "." {
		return fmt.Errorf("refusing to clear restore destination: empty or current-directory path")
	}
	if clean == string(filepath.Separator) {
		return fmt.Errorf("refusing to clear restore destination: filesystem root")
	}
	entries, err := os.ReadDir(clean)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading restore destination %s: %w", clean, err)
	}
	for _, entry := range entries {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err := os.RemoveAll(filepath.Join(clean, entry.Name())); err != nil {
			return fmt.Errorf("clearing %s: %w", filepath.Join(clean, entry.Name()), err)
		}
	}
	return nil
}
