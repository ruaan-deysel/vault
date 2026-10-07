//go:build windows

package fsstat

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// Stat returns the capacity of the volume that holds path.
func Stat(path string) (Usage, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return Usage{}, fmt.Errorf("disk free space %s: %w", path, err)
	}
	var freeToCaller, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeToCaller, &total, &totalFree); err != nil {
		return Usage{}, fmt.Errorf("disk free space %s: %w", path, err)
	}
	return Usage{Total: total, Free: freeToCaller}, nil
}
