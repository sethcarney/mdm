package commands

import "testing"

func TestCrossesMajor(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"1.9.1", "2.0.0", true},
		{"1.9.1", "1.10.0", false},
		{"2.0.0", "2.9.9", false},
		{"1.9.1", "3.0.0", true},
		{"2.0.0-rc.1", "2.0.0", false},
		// A dev build can't judge version distance — never offer.
		{"dev", "2.0.0", false},
		{"1.9.1", "dev", false},
	}
	for _, c := range cases {
		if got := crossesMajor(c.current, c.latest); got != c.want {
			t.Errorf("crossesMajor(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}
