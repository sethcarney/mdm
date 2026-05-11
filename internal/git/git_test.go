package git

import "testing"

func TestIsSemverTag(t *testing.T) {
	cases := []struct {
		tag  string
		want bool
	}{
		{"v1.0.0", true},
		{"v0.0.1", true},
		{"v10.20.30", true},
		{"1.2.3", true},
		{"v1.2.3-alpha.1", true},
		{"main", false},
		{"HEAD", false},
		{"v1.0", false},
		{"v1", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsSemverTag(tc.tag); got != tc.want {
			t.Errorf("IsSemverTag(%q) = %v, want %v", tc.tag, got, tc.want)
		}
	}
}

func TestLatestSemverTag(t *testing.T) {
	cases := []struct {
		tags []string
		want string
	}{
		{[]string{"v1.0.0", "v1.1.0", "v2.0.0"}, "v2.0.0"},
		{[]string{"v2.0.0", "v1.0.0", "v1.1.0"}, "v2.0.0"},
		{[]string{"v1.0.0", "v1.0.0-alpha.1", "v1.0.0-beta.1"}, "v1.0.0"},
		{[]string{"main", "HEAD", "v0.1.0"}, "v0.1.0"},
		{[]string{"main", "HEAD"}, ""},
		{nil, ""},
		// Pre-releases only → return none (stable releases only)
		{[]string{"v1.0.0-alpha", "v2.0.0-beta"}, ""},
		// Patch version wins
		{[]string{"v1.0.0", "v1.0.1", "v1.0.2"}, "v1.0.2"},
	}
	for _, tc := range cases {
		if got := LatestSemverTag(tc.tags); got != tc.want {
			t.Errorf("LatestSemverTag(%v) = %q, want %q", tc.tags, got, tc.want)
		}
	}
}

func TestCompareSemverTags(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.0.0", "v1.0.0", 0},
		{"v1.0.0", "v2.0.0", -1},
		{"v2.0.0", "v1.0.0", 1},
		{"v1.1.0", "v1.0.0", 1},
		{"v1.0.1", "v1.0.0", 1},
		{"v1.0.0-alpha", "v1.0.0", -1},
		{"v1.0.0", "v1.0.0-alpha", 1},
		// Non-semver inputs → 0
		{"main", "v1.0.0", 0},
		{"v1.0.0", "HEAD", 0},
	}
	for _, tc := range cases {
		got := CompareSemverTags(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("CompareSemverTags(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
