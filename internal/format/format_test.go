package format

import (
	"math"
	"testing"
)

// TestBytes pins the single project-wide rendering contract. The values here
// are the ones that used to differ between implementations: 1536 rendered as
// "2 KB" in the notification formatter but "1.5 KB" everywhere else, and a
// count just under a boundary rendered as "1024 KB" rather than promoting to
// the next unit.
func TestBytes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		bytes float64
		want  string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1 KB"},
		{1536, "1.5 KB"}, // was "2 KB"
		{2048, "2 KB"},
		{1024*1024 - 1, "1024 KB"},
		{1024 * 1024, "1 MB"},
		{1536 * 1024, "1.5 MB"},
		{1 << 30, "1 GB"},
		{1536 * 1024 * 1024, "1.5 GB"},
		{1 << 40, "1 TB"},
		{2500 * (1 << 30), "2.4 TB"},
		{1 << 50, "1 PB"},
		// Beyond the largest unit it keeps scaling PB rather than wrapping.
		{1024 * (1 << 50), "1024 PB"},
		{-1536, "-1.5 KB"},
	}
	for _, c := range cases {
		if got := Bytes(c.bytes); got != c.want {
			t.Errorf("Bytes(%v) = %q, want %q", c.bytes, got, c.want)
		}
	}
}

// TestBytesNonFinite covers inputs that can reach this from a computed rate
// (bytes divided by a duration), where a zero or missing duration can produce
// NaN or Inf. Rendering those as a number would be worse than saying nothing.
func TestBytesNonFinite(t *testing.T) {
	t.Parallel()
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if got := Bytes(v); got != "—" {
			t.Errorf("Bytes(%v) = %q, want %q", v, got, "—")
		}
	}
}
