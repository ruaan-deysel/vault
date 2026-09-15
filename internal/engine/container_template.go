package engine

import (
	"sort"
	"strings"
)

// volumePathRewrite is one host-path substitution the restore applied to a
// container's bind mounts, kept so the Unraid template can be rewritten to
// match (issue #336).
type volumePathRewrite struct {
	Old string
	New string
}

// rewriteTemplateVolumePaths rewrites the host paths inside an Unraid
// container template so they match the binds the restore actually created.
//
// A custom-destination restore rewrites every bind to restoreDest/<name>, but
// the template XML was written back byte-for-byte. The Docker page reads the
// live container for its "Volume Mappings" column and the template for its
// Edit form, so the two disagreed: the container ran against the custom
// destination while Edit still offered the original paths — and saving Edit
// silently reverted the remap (issue #336).
//
// Substitution is textual, mirroring the domain-XML rewrite in vm_restore.go:
// a template's path appears in both the Config element's text and its Default
// attribute, and one pass over the raw bytes catches both without having to
// round-trip unknown attributes, element order, and formatting through
// encoding/xml.
//
// A match only counts when the path ends at a boundary — end of input, a
// quote, an XML delimiter, or a path separator — so /mnt/user/appdata/plex
// never rewrites the unrelated /mnt/user/appdata/plex-config, while a
// subdirectory of a remapped mount follows its parent. Longer paths are
// applied first so nested mounts resolve to the most specific rewrite.
func rewriteTemplateVolumePaths(data []byte, rewrites []volumePathRewrite) []byte {
	if len(data) == 0 || len(rewrites) == 0 {
		return data
	}

	ordered := make([]volumePathRewrite, 0, len(rewrites))
	for _, rw := range rewrites {
		if rw.Old == "" || rw.New == "" || rw.Old == rw.New {
			continue
		}
		ordered = append(ordered, rw)
	}
	if len(ordered) == 0 {
		return data
	}
	sort.SliceStable(ordered, func(i, j int) bool { return len(ordered[i].Old) > len(ordered[j].Old) })

	content := string(data)
	for _, rw := range ordered {
		content = replaceAtPathBoundary(content, rw.Old, rw.New)
	}
	return []byte(content)
}

// replaceAtPathBoundary replaces every occurrence of old in s with replacement,
// but only where the occurrence ends at a path boundary — so a path never
// rewrites a longer sibling that merely starts with it.
func replaceAtPathBoundary(s, old, replacement string) string {
	var b strings.Builder
	for {
		idx := strings.Index(s, old)
		if idx < 0 {
			b.WriteString(s)
			return b.String()
		}
		end := idx + len(old)
		b.WriteString(s[:idx])
		if end == len(s) || isPathBoundary(s[end]) {
			b.WriteString(replacement)
		} else {
			b.WriteString(old)
		}
		s = s[end:]
	}
}

// isPathBoundary reports whether c can legitimately follow a complete host
// path inside a template: a separator introducing a subdirectory, or one of
// the characters that terminate an XML attribute value or element text.
func isPathBoundary(c byte) bool {
	switch c {
	case '/', '"', '\'', '<', '>', ':', ',', ' ', '\t', '\r', '\n':
		return true
	default:
		return false
	}
}
