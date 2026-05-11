package version

import (
	"strconv"
	"strings"
)

var Version = "dev" // overridden by ldflags at release build time
const AppName = "mdm"

// IsNewer reports whether latest is strictly greater than current.
// Handles semver pre-release suffixes (e.g. 1.6.0-beta3 < 1.6.0).
func IsNewer(latest, current string) bool {
	lVer, lPre := splitSemver(latest)
	cVer, cPre := splitSemver(current)

	for i := 0; i < 3; i++ {
		l := versionPart(lVer, i)
		c := versionPart(cVer, i)
		if l > c {
			return true
		}
		if l < c {
			return false
		}
	}

	// major.minor.patch are equal; apply semver pre-release precedence:
	// stable (no pre-release) > any pre-release
	switch {
	case lPre == "" && cPre != "":
		return true
	case lPre != "" && cPre == "":
		return false
	case lPre == "" && cPre == "":
		return false
	default:
		return comparePre(lPre, cPre) > 0
	}
}

func splitSemver(v string) (ver, pre string) {
	v = strings.TrimPrefix(v, "v")
	if idx := strings.IndexByte(v, '-'); idx >= 0 {
		return v[:idx], v[idx+1:]
	}
	return v, ""
}

func versionPart(ver string, i int) int {
	parts := strings.SplitN(ver, ".", 3)
	if i < len(parts) {
		n, _ := strconv.Atoi(parts[i])
		return n
	}
	return 0
}

// comparePre compares two pre-release identifiers (e.g. "beta3", "rc1").
// Returns negative, zero, or positive.
func comparePre(a, b string) int {
	aLabel, aNum := splitPreLabel(a)
	bLabel, bNum := splitPreLabel(b)
	if aLabel < bLabel {
		return -1
	}
	if aLabel > bLabel {
		return 1
	}
	if aNum < bNum {
		return -1
	}
	if aNum > bNum {
		return 1
	}
	return 0
}

func splitPreLabel(s string) (label string, num int) {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	num, _ = strconv.Atoi(s[i:])
	return s[:i], num
}
