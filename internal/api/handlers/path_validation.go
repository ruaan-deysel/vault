package handlers

import (
	"fmt"
	"strings"

	"github.com/ruaan-deysel/vault/internal/safepath"
)

var browseAllowedRoots = []string{"/mnt", "/boot"}

var configurablePathRoots = []string{"/mnt", "/boot", "/tmp"}

var restoreAllowedRoots = []string{"/mnt", "/boot", "/tmp", "/etc", "/opt", "/usr/local", "/var", "/home", "/private/var", "/private/tmp"}

func normalizeConfigurablePath(path string) (string, error) {
	return safepath.NormalizeAbsoluteUnderRoots(path, configurablePathRoots)
}

func normalizeRestoreDestination(path string) (string, error) {
	if strings.Contains(path, "../") || strings.Contains(path, "..\\") {
		return "", fmt.Errorf("path traversal not allowed")
	}
	for _, part := range strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if part == ".." {
			return "", fmt.Errorf("path traversal not allowed")
		}
	}
	return safepath.NormalizeAbsoluteUnderRoots(path, restoreAllowedRoots)
}
