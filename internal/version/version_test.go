package version

import "testing"

func TestMajor(t *testing.T) {
	cases := []struct {
		in    string
		major int
		ok    bool
	}{
		{"1.9.1", 1, true},
		{"v2.0.0", 2, true},
		{"2.0.0-rc.1", 2, true},
		{"10.4", 10, true},
		{"dev", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := Major(c.in)
		if got != c.major || ok != c.ok {
			t.Errorf("Major(%q) = (%d, %v), want (%d, %v)", c.in, got, ok, c.major, c.ok)
		}
	}
}

func TestIsNewerAcrossMajors(t *testing.T) {
	if !IsNewer("2.0.0", "1.9.1") {
		t.Error("2.0.0 should be newer than 1.9.1")
	}
	if IsNewer("1.9.1", "2.0.0") {
		t.Error("1.9.1 should not be newer than 2.0.0")
	}
}
