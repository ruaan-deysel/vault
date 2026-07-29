package notify

import "fmt"

// FormatBytes returns a human-readable string for the given byte count.
// It uses binary (1024-based) thresholds: B, KB, MB, GB, TB.
// Values at or above 1024 of the current unit promote to the next unit.
// The largest unit shown with one decimal place; the smallest (bytes) shown
// as an integer.
func FormatBytes(bytes int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
		tb = gb * 1024
	)
	switch {
	case bytes >= tb:
		return fmt.Sprintf("%.1f TB", float64(bytes)/float64(tb))
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.0f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
