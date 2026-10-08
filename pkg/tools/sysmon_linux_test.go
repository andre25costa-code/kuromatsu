//go:build linux

package tools

import "testing"

func TestFormatBytes(t *testing.T) {
	cases := map[uint64]string{
		0:                  "0 B",
		1023:               "1023 B",
		1024:               "1.0 KiB",
		1024 * 1024:        "1.0 MiB",
		1536 * 1024 * 1024: "1.5 GiB",
	}
	for in, want := range cases {
		if got := formatBytes(in); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
