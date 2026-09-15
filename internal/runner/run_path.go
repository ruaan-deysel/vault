package runner

import (
	"fmt"
	"log"

	"github.com/ruaan-deysel/vault/internal/db"
	"github.com/ruaan-deysel/vault/internal/storage"
)

// maxRunPathAttempts bounds the disambiguation search. Reaching it means
// something other than a same-second collision is going on, and silently
// looping forever would be worse than falling back to the plain name.
const maxRunPathAttempts = 50

// uniqueRunPath returns basePath, or the first "<basePath>-N" that does not
// already exist at dest.
//
// The run ID used to lead the run folder name and was the only thing
// guaranteeing it was unique. Dropping it for issue #319 leaves a
// second-resolution timestamp, which two back-to-back manual runs of a fast
// job can share — and the storage adapters overwrite silently, so a collision
// would destroy the earlier run's data with no error anywhere.
//
// A destination that cannot be reached is not a reason to refuse the backup:
// the run proceeds on the plain name and the upload surfaces the real problem
// with a far better message than this check could.
func uniqueRunPath(dest db.StorageDestination, basePath string) string {
	adapter, err := storage.NewAdapter(dest.Type, dest.Config)
	if err != nil {
		log.Printf("runner: cannot check %s for an existing run folder: %v (using %s as-is)", dest.Name, err, basePath)
		return basePath
	}
	defer storage.CloseAdapter(adapter)

	candidate := basePath
	for attempt := 2; attempt <= maxRunPathAttempts; attempt++ {
		entries, listErr := adapter.List(candidate)
		if listErr != nil {
			// Not found is the common answer and the one we want; anything
			// else is a destination problem the upload will report properly.
			return candidate
		}
		if len(entries) == 0 {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", basePath, attempt)
	}

	log.Printf("runner: %s already has %d run folders for this timestamp — reusing %s", dest.Name, maxRunPathAttempts, candidate)
	return candidate
}
