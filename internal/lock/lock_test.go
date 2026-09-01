package lock

import (
	"os"
	"strings"
	"testing"
)

func TestInstallModeAccessorsProjectScope(t *testing.T) {
	cwd := t.TempDir()
	if got := GetInstallMode(false, cwd); got != "" {
		t.Fatalf("default mode = %q, want empty", got)
	}
	if err := SetInstallMode(InstallModeCopy, false, cwd); err != nil {
		t.Fatal(err)
	}
	if got := GetInstallMode(false, cwd); got != InstallModeCopy {
		t.Fatalf("mode = %q, want %q", got, InstallModeCopy)
	}
}

func TestSetInstallModePreservesOtherSections(t *testing.T) {
	cwd := t.TempDir()
	content := `{"version":2,"skills":{"s":{"source":"o/r","sourceType":"github"}},"futureFlag":true}`
	if err := os.WriteFile(GetProjectLockPath(cwd), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SetInstallMode(InstallModeCopy, false, cwd); err != nil {
		t.Fatal(err)
	}
	lk := ReadProjectLock(cwd)
	if _, ok := lk.Skills["s"]; !ok {
		t.Error("skills section lost")
	}
	data, _ := os.ReadFile(GetProjectLockPath(cwd))
	if !strings.Contains(string(data), "futureFlag") {
		t.Error("unknown key lost")
	}
}

func TestInstallModeAccessorsGlobalScope(t *testing.T) {
	isolateGlobal(t)
	if err := SetInstallMode(InstallModeCopy, true, ""); err != nil {
		t.Fatal(err)
	}
	if got := GetInstallMode(true, ""); got != InstallModeCopy {
		t.Fatalf("mode = %q, want %q", got, InstallModeCopy)
	}
}
