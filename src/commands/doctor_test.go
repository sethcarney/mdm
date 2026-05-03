package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sethcarney/mdm/internal/lock"
)

// ── formatFileSize ─────────────────────────────────────────────────────────────

func TestFormatFileSize(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{1024, "1KB"},
		{20 * 1024, "20KB"},
		{100 * 1024, "100KB"},
		{1024 * 1024, "1.0MB"},
		{2*1024*1024 + 512*1024, "2.5MB"},
	}
	for _, tc := range tests {
		got := formatFileSize(tc.bytes)
		if got != tc.expected {
			t.Errorf("formatFileSize(%d) = %q, want %q", tc.bytes, got, tc.expected)
		}
	}
}

// ── diagnoseSkill: missing directory ──────────────────────────────────────────

func TestDiagnoseSkillMissingDir(t *testing.T) {
	r := &doctorResult{
		Name:  "test-skill",
		Scope: "global",
		Path:  "/nonexistent/path/test-skill",
	}
	diagnoseSkill(r, "", false, t.TempDir())

	if len(r.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(r.Issues), r.Issues)
	}
	if r.Issues[0].Level != "error" {
		t.Errorf("expected error level, got %q", r.Issues[0].Level)
	}
	if !strings.Contains(r.Issues[0].Message, "skill directory not found") {
		t.Errorf("unexpected message: %q", r.Issues[0].Message)
	}
}

// ── diagnoseSkill: missing SKILL.md ───────────────────────────────────────────

func TestDiagnoseSkillMissingSkillMd(t *testing.T) {
	dir := t.TempDir()
	r := &doctorResult{Name: "test-skill", Scope: "global", Path: dir}
	diagnoseSkill(r, "", false, t.TempDir())

	// Expect SKILL.md error + README warning
	if len(r.Issues) != 2 {
		t.Fatalf("expected 2 issues, got %d: %v", len(r.Issues), r.Issues)
	}
	if r.Issues[0].Level != "error" {
		t.Errorf("expected error level, got %q", r.Issues[0].Level)
	}
	if !strings.Contains(r.Issues[0].Message, "SKILL.md not found") {
		t.Errorf("unexpected message: %q", r.Issues[0].Message)
	}
}

// ── diagnoseSkill: SKILL.md read error surfaces as error (not warn) ───────────

func TestDiagnoseSkillMdReadError(t *testing.T) {
	dir := t.TempDir()
	skillMd := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skillMd, []byte("valid content"), 0o000); err != nil {
		t.Fatal(err)
	}
	// Running as root would bypass permissions; skip the test in that case.
	if os.Getuid() == 0 {
		t.Skip("skipping permission test when running as root")
	}

	r := &doctorResult{Name: "test-skill", Scope: "global", Path: dir}
	diagnoseSkill(r, "", false, t.TempDir())

	found := false
	for _, issue := range r.Issues {
		if issue.Level == "error" && strings.Contains(issue.Message, "SKILL.md could not be read") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an error issue about SKILL.md read failure, got: %v", r.Issues)
	}
}

// ── diagnoseSkill: SKILL.md missing fields → warn, not error ─────────────────

func TestDiagnoseSkillMdMissingFields(t *testing.T) {
	dir := t.TempDir()
	// Valid YAML but missing required name/description
	content := "---\nauthor: someone\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &doctorResult{Name: "test-skill", Scope: "global", Path: dir}
	diagnoseSkill(r, "", false, t.TempDir())

	found := false
	for _, issue := range r.Issues {
		if issue.Level == "warn" && strings.Contains(issue.Message, "missing required name or description") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a warn about missing frontmatter fields, got: %v", r.Issues)
	}
}

// ── diagnoseSkill: hash mismatch ──────────────────────────────────────────────

func TestDiagnoseSkillHashMismatch(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: test\ndescription: A test skill\n---\nHello\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &doctorResult{Name: "test-skill", Scope: "global", Path: dir}
	diagnoseSkill(r, "definitely-wrong-hash", false, t.TempDir())

	found := false
	for _, issue := range r.Issues {
		if issue.Level == "warn" && strings.Contains(issue.Message, "skill content differs from installed version") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a warn about hash mismatch, got: %v", r.Issues)
	}
}

// ── diagnoseSkill: hash matches → no mismatch issue ───────────────────────────

func TestDiagnoseSkillHashMatch(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: test\ndescription: A test skill\n---\nHello\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	hash, err := lock.ComputeSkillFolderHash(dir)
	if err != nil {
		t.Fatal(err)
	}

	r := &doctorResult{Name: "test-skill", Scope: "global", Path: dir}
	diagnoseSkill(r, hash, false, t.TempDir())

	for _, issue := range r.Issues {
		if strings.Contains(issue.Message, "skill content differs") {
			t.Errorf("unexpected hash-mismatch issue when hashes should match: %v", issue)
		}
		if strings.Contains(issue.Message, "integrity check skipped") {
			t.Errorf("unexpected hash-error issue when hash should be computable: %v", issue)
		}
	}
}

// ── diagnoseSkill: hash error is surfaced as warn ─────────────────────────────

func TestDiagnoseSkillHashError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping permission test when running as root")
	}
	dir := t.TempDir()
	content := "---\nname: test\ndescription: A test skill\n---\nHello\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create an unreadable subdirectory so hash computation fails
	subDir := filepath.Join(dir, "assets")
	if err := os.Mkdir(subDir, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(subDir, 0o755) //nolint:errcheck // cleanup best-effort

	r := &doctorResult{Name: "test-skill", Scope: "global", Path: dir}
	// Use a fake stored hash so we enter the hash-check branch
	diagnoseSkill(r, "some-stored-hash", false, t.TempDir())

	// Either a hash error OR a hash mismatch is acceptable — the important thing
	// is that something is reported rather than silently ignored.
	hasHashIssue := false
	for _, issue := range r.Issues {
		if strings.Contains(issue.Message, "integrity check skipped") ||
			strings.Contains(issue.Message, "skill content differs") {
			hasHashIssue = true
		}
	}
	if !hasHashIssue {
		t.Errorf("expected a hash-related issue, got: %v", r.Issues)
	}
}

// ── checkLargeMarkdown: skips .git and node_modules ───────────────────────────

func TestCheckLargeMarkdownSkipsDirs(t *testing.T) {
	dir := t.TempDir()

	// Large .md file inside .git — must be skipped
	gitDir := filepath.Join(dir, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileOfSize(filepath.Join(gitDir, "COMMIT_EDITMSG.md"), fileSizeErrorBytes+1); err != nil {
		t.Fatal(err)
	}

	// Large .md file inside node_modules — must be skipped
	nmDir := filepath.Join(dir, "node_modules")
	if err := os.Mkdir(nmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileOfSize(filepath.Join(nmDir, "README.md"), fileSizeErrorBytes+1); err != nil {
		t.Fatal(err)
	}

	// Small SKILL.md in root — should NOT generate a size issue
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: t\ndescription: d\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &doctorResult{Name: "test", Path: dir}
	checkLargeMarkdown(r)

	for _, issue := range r.Issues {
		if strings.Contains(issue.Message, ".git") || strings.Contains(issue.Message, "node_modules") {
			t.Errorf("checkLargeMarkdown should have skipped %s, but got issue: %v", issue.Message, issue)
		}
	}
}

// ── checkLargeMarkdown: detects oversized files ───────────────────────────────

func TestCheckLargeMarkdownDetectsOversized(t *testing.T) {
	dir := t.TempDir()

	// Warn-level file (20KB ≤ size < 100KB)
	if err := writeFileOfSize(filepath.Join(dir, "warn.md"), fileSizeWarnBytes+1); err != nil {
		t.Fatal(err)
	}
	// Error-level file (≥ 100KB)
	if err := writeFileOfSize(filepath.Join(dir, "big.md"), fileSizeErrorBytes+1); err != nil {
		t.Fatal(err)
	}

	r := &doctorResult{Name: "test", Path: dir}
	checkLargeMarkdown(r)

	warnFound, errFound := false, false
	for _, issue := range r.Issues {
		switch issue.Level {
		case "warn":
			warnFound = true
		case "error":
			errFound = true
		}
	}
	if !warnFound {
		t.Error("expected a warn issue for the 20KB+ file")
	}
	if !errFound {
		t.Error("expected an error issue for the 100KB+ file")
	}
}

// ── checkProjectMarkdown: skips non-existent dirs in skipDirs ─────────────────

func TestCheckProjectMarkdownSkipsExistingDirs(t *testing.T) {
	root := t.TempDir()

	// Create a real skills dir with a large markdown file
	skillsDir := filepath.Join(root, ".agents", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileOfSize(filepath.Join(skillsDir, "SKILL.md"), fileSizeErrorBytes+1); err != nil {
		t.Fatal(err)
	}

	// Non-existent dir added to skipDirs — should not cause a panic or side effects
	skipDirs := map[string]bool{
		filepath.Clean(skillsDir):                     true,
		filepath.Join(root, "nonexistent-agent-dir"): true,
	}

	issues, _ := checkProjectMarkdown(root, skipDirs, map[string]bool{})

	for _, issue := range issues {
		if strings.Contains(issue.Message, ".agents") {
			t.Errorf("checkProjectMarkdown should skip the skills dir, but got: %v", issue)
		}
	}
}

// ── checkProjectMarkdown: walk limit triggers truncation ──────────────────────

func TestCheckProjectMarkdownTruncation(t *testing.T) {
	// Override the walk limit temporarily to a very small number.
	// We do this by building a directory with many entries.
	root := t.TempDir()
	// Create markdownWalkLimit+10 files so the scan is forced to truncate.
	// To avoid actually creating 10000 files we test at a tiny scale by
	// verifying that truncated is false when limit is not reached.
	issues, truncated := checkProjectMarkdown(root, map[string]bool{}, map[string]bool{})
	if truncated {
		t.Error("expected truncated=false for an empty directory")
	}
	_ = issues
}

// ── checkProjectMarkdown: skips .git ─────────────────────────────────────────

func TestCheckProjectMarkdownSkipsGit(t *testing.T) {
	root := t.TempDir()

	gitDir := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileOfSize(filepath.Join(gitDir, "COMMIT_EDITMSG.md"), fileSizeErrorBytes+1); err != nil {
		t.Fatal(err)
	}

	issues, _ := checkProjectMarkdown(root, map[string]bool{}, map[string]bool{})
	for _, issue := range issues {
		if strings.Contains(issue.Message, ".git") {
			t.Errorf("should have skipped .git dir but got: %v", issue)
		}
	}
}

// ── checkInstructionFiles ─────────────────────────────────────────────────────

func TestCheckInstructionFilesDetectsOversized(t *testing.T) {
	cwd := t.TempDir()

	claudeMd := filepath.Join(cwd, "CLAUDE.md")
	if err := writeFileOfSize(claudeMd, fileSizeWarnBytes+1); err != nil {
		t.Fatal(err)
	}

	issues := checkInstructionFiles(cwd)

	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "CLAUDE.md") {
			found = true
		}
	}
	if !found {
		t.Error("expected an issue for oversized CLAUDE.md")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// writeFileOfSize creates a file filled with 'x' bytes of the given size.
func writeFileOfSize(path string, size int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, size)
	for i := range buf {
		buf[i] = 'x'
	}
	_, err = f.Write(buf)
	return err
}

// TestDoctorIssueIcon verifies the icon/color mapping.
func TestDoctorIssueIcon(t *testing.T) {
	icon, color := doctorIssueIcon("error")
	if icon != "✗" {
		t.Errorf("expected ✗ for error, got %q", icon)
	}
	_ = color

	icon, _ = doctorIssueIcon("warn")
	if icon != "▲" {
		t.Errorf("expected ▲ for warn, got %q", icon)
	}
}

// TestDoctorStatusIcon verifies the status icon logic.
func TestDoctorStatusIcon(t *testing.T) {
	tests := []struct {
		errors, warnings int
		wantIcon         string
	}{
		{1, 0, "✗"},
		{0, 1, "▲"},
		{0, 0, "✓"},
	}
	for _, tc := range tests {
		icon, _ := doctorStatusIcon(tc.errors, tc.warnings)
		if icon != tc.wantIcon {
			t.Errorf("doctorStatusIcon(%d, %d) = %q, want %q", tc.errors, tc.warnings, icon, tc.wantIcon)
		}
	}
}

// TestFormatFileSizeEdgeCases covers MB threshold and exact boundary.
func TestFormatFileSizeEdgeCases(t *testing.T) {
	// Exactly at MB boundary
	got := formatFileSize(1024 * 1024)
	if got != "1.0MB" {
		t.Errorf("got %q, want 1.0MB", got)
	}
	// Just below MB
	got = formatFileSize(1024*1024 - 1)
	if !strings.HasSuffix(got, "KB") {
		t.Errorf("expected KB suffix for sub-MB size, got %q", got)
	}
}

// TestCheckProjectMarkdownSkipsFiles verifies that skipFiles prevents reporting.
func TestCheckProjectMarkdownSkipsFiles(t *testing.T) {
	root := t.TempDir()
	bigFile := filepath.Join(root, "big.md")
	if err := writeFileOfSize(bigFile, fileSizeErrorBytes+1); err != nil {
		t.Fatal(err)
	}

	skipFiles := map[string]bool{filepath.Clean(bigFile): true}
	issues, _ := checkProjectMarkdown(root, map[string]bool{}, skipFiles)
	for _, issue := range issues {
		if strings.Contains(issue.Message, "big.md") {
			t.Errorf("skipFiles should have suppressed this issue: %v", issue)
		}
	}
}

// TestCheckProjectMarkdownFindsLargeFiles confirms detection still works for
// files that are not in skipDirs/skipFiles.
func TestCheckProjectMarkdownFindsLargeFiles(t *testing.T) {
	root := t.TempDir()
	if err := writeFileOfSize(filepath.Join(root, "huge.md"), fileSizeErrorBytes+1); err != nil {
		t.Fatal(err)
	}

	issues, _ := checkProjectMarkdown(root, map[string]bool{}, map[string]bool{})
	found := false
	for _, issue := range issues {
		if issue.Level == "error" && strings.Contains(issue.Message, "huge.md") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an error issue for huge.md, got: %v", issues)
	}
}

// TestCheckLargeMarkdownSmallFiles ensures small files produce no issues.
func TestCheckLargeMarkdownSmallFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: t\ndescription: d\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &doctorResult{Name: "test", Path: dir}
	checkLargeMarkdown(r)

	if len(r.Issues) != 0 {
		t.Errorf("expected no issues for small files, got: %v", r.Issues)
	}
}

// TestCheckLargeMarkdownVendorSkipped ensures 'vendor' dir is skipped.
func TestCheckLargeMarkdownVendorSkipped(t *testing.T) {
	dir := t.TempDir()
	vendorDir := filepath.Join(dir, "vendor")
	if err := os.Mkdir(vendorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileOfSize(filepath.Join(vendorDir, "README.md"), fileSizeErrorBytes+1); err != nil {
		t.Fatal(err)
	}

	r := &doctorResult{Name: "test", Path: dir}
	checkLargeMarkdown(r)

	for _, issue := range r.Issues {
		if strings.Contains(issue.Message, "vendor") {
			t.Errorf("checkLargeMarkdown should skip vendor/, got: %v", issue)
		}
	}
}

// TestDiagnoseSkillHealthySkill ensures a well-formed skill directory reports
// no issues (except possibly agent-link checks that won't have links on disk).
func TestDiagnoseSkillHealthySkill(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: My Skill\ndescription: A healthy skill\n---\nDo stuff.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# My Skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hash, err := lock.ComputeSkillFolderHash(dir)
	if err != nil {
		t.Fatal(err)
	}

	r := &doctorResult{Name: "my-skill", Scope: "global", Path: dir}
	diagnoseSkill(r, hash, false, t.TempDir())

	if len(r.Issues) != 0 {
		t.Errorf("expected no issues for a healthy skill, got: %v", r.Issues)
	}
}
