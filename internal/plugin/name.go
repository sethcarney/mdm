package plugin

import (
	"fmt"
	"strings"
)

func isNameAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

// ValidateName enforces the spec's plugin-name rules: 1-64 characters of
// lowercase alphanumerics, hyphens, and periods; alphanumeric first and
// last character; no consecutive hyphens or periods.
func ValidateName(name string) error {
	if len(name) == 0 || len(name) > 64 {
		return fmt.Errorf("must be 1-64 characters")
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !isNameAlnum(c) && c != '-' && c != '.' {
			return fmt.Errorf("may only contain lowercase alphanumerics, hyphens, and periods")
		}
	}
	if !isNameAlnum(name[0]) || !isNameAlnum(name[len(name)-1]) {
		return fmt.Errorf("must start and end with an alphanumeric character")
	}
	if strings.Contains(name, "--") || strings.Contains(name, "..") {
		return fmt.Errorf("must not contain consecutive hyphens or periods")
	}
	return nil
}
