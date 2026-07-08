package okf

import (
	"path/filepath"
	"testing"
)

func loadFixture(t *testing.T, name string) *Bundle {
	t.Helper()
	b, err := LoadBundle(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("LoadBundle(%s): %v", name, err)
	}
	return b
}

func rulesOf(issues []Issue) map[string]int {
	counts := map[string]int{}
	for _, issue := range issues {
		counts[issue.Rule]++
	}
	return counts
}

func TestLoadBundleParsesDocuments(t *testing.T) {
	b := loadFixture(t, "valid-bundle")
	if len(b.Docs) != 3 {
		t.Fatalf("expected 3 documents, got %d", len(b.Docs))
	}

	var orders *Document
	for _, d := range b.Docs {
		if d.RelPath == "tables/orders.md" {
			orders = d
		}
	}
	if orders == nil {
		t.Fatal("expected tables/orders.md in bundle")
	}
	if orders.Type != "BigQuery Table" {
		t.Errorf("type = %q, want %q", orders.Type, "BigQuery Table")
	}
	if orders.Title != "Orders" {
		t.Errorf("title = %q, want %q", orders.Title, "Orders")
	}
	if len(orders.Tags) != 2 || orders.Tags[0] != "sales" {
		t.Errorf("tags = %v, want [sales revenue]", orders.Tags)
	}
	if orders.Timestamp != "2026-05-28T14:30:00Z" {
		t.Errorf("timestamp = %q, want 2026-05-28T14:30:00Z", orders.Timestamp)
	}
	// Two markdown links + one external in the body; all are captured raw.
	if len(orders.Links) != 2 {
		t.Errorf("links = %v, want 2 entries", orders.Links)
	}
}

func TestValidateCleanBundle(t *testing.T) {
	issues := Validate(loadFixture(t, "valid-bundle"))
	if len(issues) != 0 {
		t.Errorf("expected no issues for valid bundle, got %v", issues)
	}
}

func TestValidateMissingType(t *testing.T) {
	issues := Validate(loadFixture(t, "missing-type"))
	if rulesOf(issues)["missing-type"] != 1 {
		t.Errorf("expected one missing-type error, got %v", issues)
	}
	if !HasErrors(issues) {
		t.Error("expected HasErrors to be true")
	}
	for _, issue := range issues {
		if issue.Rule == "missing-type" && issue.File != "concepts/no-type.md" {
			t.Errorf("missing-type anchored to %q, want concepts/no-type.md", issue.File)
		}
	}
}

func TestValidateBrokenLink(t *testing.T) {
	issues := Validate(loadFixture(t, "broken-link"))
	if rulesOf(issues)["broken-link"] != 1 {
		t.Errorf("expected one broken-link error, got %v", issues)
	}
}

func TestValidateEscapingLink(t *testing.T) {
	issues := Validate(loadFixture(t, "escape-link"))
	if rulesOf(issues)["link-escapes-bundle"] != 1 {
		t.Errorf("expected one link-escapes-bundle error, got %v", issues)
	}
}

func TestValidateOrphan(t *testing.T) {
	issues := Validate(loadFixture(t, "orphan"))
	counts := rulesOf(issues)
	if counts["orphaned-document"] != 1 {
		t.Errorf("expected one orphaned-document warning, got %v", issues)
	}
	if counts["invalid-timestamp"] != 1 {
		t.Errorf("expected one invalid-timestamp warning, got %v", issues)
	}
	// Warnings alone must not fail validation.
	if HasErrors(issues) {
		t.Errorf("expected warnings only, got %v", issues)
	}
}

func TestValidateEmptyBundle(t *testing.T) {
	issues := Validate(&Bundle{Root: t.TempDir()})
	if rulesOf(issues)["empty-bundle"] != 1 || !HasErrors(issues) {
		t.Errorf("expected empty-bundle error, got %v", issues)
	}
}

func TestLoadBundleMissingDir(t *testing.T) {
	if _, err := LoadBundle(filepath.Join("testdata", "does-not-exist")); err == nil {
		t.Error("expected error for missing directory")
	}
}

func TestResolveLink(t *testing.T) {
	cases := []struct {
		from, target string
		want         string
		inside       bool
	}{
		{"index.md", "tables/orders.md", "tables/orders.md", true},
		{"tables/orders.md", "../metrics/wau.md", "metrics/wau.md", true},
		{"tables/orders.md", "/metrics/wau.md", "metrics/wau.md", true},
		{"tables/orders.md", "/metrics/wau.md#definition", "metrics/wau.md", true},
		{"index.md", "../outside.md", "../outside.md", false},
		{"a/b/c.md", "../../../x.md", "../x.md", false},
	}
	for _, c := range cases {
		got, inside := resolveLink(c.from, c.target)
		if got != c.want || inside != c.inside {
			t.Errorf("resolveLink(%q, %q) = (%q, %v), want (%q, %v)", c.from, c.target, got, inside, c.want, c.inside)
		}
	}
}
