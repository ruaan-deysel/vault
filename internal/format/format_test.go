package format

import "testing"

func TestFormatBytes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1 KB"},
		{2048, "2 KB"},
		{1024 * 1024, "1.0 MB"},
		{1536 * 1024, "1.5 MB"},
		{int64(1024) * 1024 * 1024, "1.0 GB"},
		{int64(1536) * 1024 * 1024, "1.5 GB"},
		{int64(1024) * 1024 * 1024 * 1024, "1.0 TB"},
		{int64(2500) * 1024 * 1024 * 1024, "2.4 TB"},
	}
	for _, c := range cases {
		if got := FormatBytes(c.bytes); got != c.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", c.bytes, got, c.want)
		}
	}
}
