package ui

import "testing"

func TestClipToWidth(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		max  int
		want string
	}{
		{"fits", "Cloning repo...", 40, "Cloning repo..."},
		{"exact", "abc", 3, "abc"},
		{"clipped", "Cloning https://github.com/owner/repo.git...", 10, "Cloning h…"},
		{"tiny width", "abc", 1, "…"},
		{"zero width keeps message", "abc", 0, "abc"},
		{"multibyte runes", "⠋⠙⠹⠸⠼", 3, "⠋⠙…"},
	}
	for _, c := range cases {
		if got := clipToWidth(c.msg, c.max); got != c.want {
			t.Errorf("%s: clipToWidth(%q, %d) = %q, want %q", c.name, c.msg, c.max, got, c.want)
		}
	}
}
