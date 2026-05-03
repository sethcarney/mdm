package source

import "testing"

func FuzzParseSource(f *testing.F) {
	seeds := []string{
		"github.com/foo/bar",
		"github.com/foo/bar.git",
		"https://github.com/foo/bar",
		"https://github.com/foo/bar/tree/main",
		"https://github.com/foo/bar/tree/main/skills",
		"github:foo/bar",
		"gitlab:foo/bar",
		"https://gitlab.com/foo/bar",
		"https://gitlab.com/foo/bar/-/tree/main",
		"https://gitlab.com/foo/bar/-/tree/main/skills",
		"foo/bar",
		"foo/bar/subpath",
		"foo/bar@skill-name",
		"foo/bar#main",
		"foo/bar#main@skill",
		"./local/path",
		"../relative/path",
		"/absolute/path",
		".",
		"..",
		"C:\\Windows\\Path",
		"git@github.com:foo/bar.git",
		"https://example.com/agent-skills",
		"coinbase/agentWallet",
		"",
		"#",
		"#@",
		"github.com/",
		"/",
		"://",
		"http://",
		"https://",
		"\x00",
		"\n",
		"foo/bar#" + string([]byte{0x00}),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		ParseSource(input) // must not panic
	})
}

func FuzzIsLocalPath(f *testing.F) {
	f.Add("./foo")
	f.Add("../foo")
	f.Add("/abs/path")
	f.Add(".")
	f.Add("..")
	f.Add("C:\\foo")
	f.Add("relative")
	f.Add("")
	f.Add("\x00")
	f.Fuzz(func(t *testing.T, input string) {
		IsLocalPath(input) // must not panic
	})
}

func FuzzSanitizeSubpath(f *testing.F) {
	f.Add("valid/path")
	f.Add("../traversal")
	f.Add("../../double")
	f.Add("foo/../bar")
	f.Add("\\backslash\\path")
	f.Add("")
	f.Add(".")
	f.Add("..")
	f.Add("\x00")
	f.Fuzz(func(t *testing.T, input string) {
		SanitizeSubpath(input) // must not panic
	})
}
