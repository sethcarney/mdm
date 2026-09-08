package commands

import (
	"errors"
	"reflect"
	"testing"

	"github.com/sethcarney/mdm/internal/source"
)

// gitCandidate builds a GitHub-sourced candidate pinned to ref.
func gitCandidate(name, src, ref string) updateCandidate {
	return updateCandidate{
		lockName:   name,
		filterName: name,
		source:     src,
		sourceType: string(source.SourceTypeGitHub),
		ref:        ref,
	}
}

// upgradeTo returns a check that always reports an available upgrade to newRef.
func upgradeTo(newRef string) upToDateCheck {
	return func(updateCandidate) (bool, string, error) { return false, newRef, nil }
}

// TestPlanUpdatesGroupsOneRepoIntoOneFetch is the regression this whole change
// exists for: N skills from the same repo must produce a single group, so the
// repo is cloned once instead of N times.
func TestPlanUpdatesGroupsOneRepoIntoOneFetch(t *testing.T) {
	var candidates []updateCandidate
	for _, name := range []string{"alpha", "bravo", "charlie", "delta"} {
		candidates = append(candidates, gitCandidate(name, "acme/skills", "v1.0.0"))
	}

	var stats updateStats
	groups := planUpdates(candidates, upgradeTo("v1.1.0"), &stats)

	if len(groups) != 1 {
		t.Fatalf("expected 1 group for 4 skills from one repo, got %d: %+v", len(groups), groups)
	}
	if groups[0].source != "acme/skills#v1.1.0" {
		t.Errorf("source = %q, want %q", groups[0].source, "acme/skills#v1.1.0")
	}
	want := []string{"alpha", "bravo", "charlie", "delta"}
	if !reflect.DeepEqual(groups[0].skills, want) {
		t.Errorf("skills = %v, want %v", groups[0].skills, want)
	}
	if stats.skipped != 0 {
		t.Errorf("skipped = %d, want 0", stats.skipped)
	}
}

func TestPlanUpdatesSeparatesDistinctRepos(t *testing.T) {
	candidates := []updateCandidate{
		gitCandidate("alpha", "acme/skills", "v1.0.0"),
		gitCandidate("bravo", "other/skills", "v2.0.0"),
		gitCandidate("charlie", "acme/skills", "v1.0.0"),
	}

	var stats updateStats
	groups := planUpdates(candidates, upgradeTo("v9.9.9"), &stats)

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d: %+v", len(groups), groups)
	}
	// Groups keep first-appearance order so output is stable across runs.
	if groups[0].source != "acme/skills#v9.9.9" || !reflect.DeepEqual(groups[0].skills, []string{"alpha", "charlie"}) {
		t.Errorf("group 0 = %+v", groups[0])
	}
	if groups[1].source != "other/skills#v9.9.9" || !reflect.DeepEqual(groups[1].skills, []string{"bravo"}) {
		t.Errorf("group 1 = %+v", groups[1])
	}
}

// A lock entry whose source already carries a #ref fragment must group with an
// otherwise identical entry that keeps the ref in the dedicated field.
func TestPlanUpdatesNormalizesExistingRefFragment(t *testing.T) {
	candidates := []updateCandidate{
		gitCandidate("alpha", "acme/skills#v1.0.0", "v1.0.0"),
		gitCandidate("bravo", "acme/skills", "v1.0.0"),
	}

	var stats updateStats
	groups := planUpdates(candidates, upgradeTo("v1.2.0"), &stats)

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d: %+v", len(groups), groups)
	}
	if groups[0].source != "acme/skills#v1.2.0" {
		t.Errorf("source = %q, want %q", groups[0].source, "acme/skills#v1.2.0")
	}
}

func TestPlanUpdatesSkipsUnupdatableSources(t *testing.T) {
	candidates := []updateCandidate{
		{lockName: "local-one", filterName: "local-one", source: "./skills", sourceType: string(source.SourceTypeLocal)},
		{lockName: "well-known", filterName: "well-known", source: "https://example.com", sourceType: string(source.SourceTypeWellKnown)},
		gitCandidate("alpha", "acme/skills", "v1.0.0"),
	}

	var stats updateStats
	groups := planUpdates(candidates, upgradeTo("v1.1.0"), &stats)

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d: %+v", len(groups), groups)
	}
	if stats.skipped != 2 {
		t.Errorf("skipped = %d, want 2", stats.skipped)
	}
}

func TestPlanUpdatesSkipsUpToDateAndErrored(t *testing.T) {
	candidates := []updateCandidate{
		gitCandidate("current", "acme/skills", "v2.0.0"),
		gitCandidate("broken", "busted/skills", "v1.0.0"),
		gitCandidate("stale", "acme/skills", "v1.0.0"),
	}

	check := func(c updateCandidate) (bool, string, error) {
		switch c.lockName {
		case "current":
			return true, "", nil
		case "broken":
			return false, "", errors.New("no stable version tags found on remote")
		default:
			return false, "v2.0.0", nil
		}
	}

	var stats updateStats
	groups := planUpdates(candidates, check, &stats)

	if len(groups) != 1 || !reflect.DeepEqual(groups[0].skills, []string{"stale"}) {
		t.Fatalf("groups = %+v, want a single group holding only \"stale\"", groups)
	}
	if stats.skipped != 2 {
		t.Errorf("skipped = %d, want 2", stats.skipped)
	}
}

// When the check reports an update with no replacement tag, the candidate's own
// ref is re-applied rather than dropped - re-fetching an unpinned default branch
// would silently change what is installed.
func TestPlanUpdatesKeepsCurrentRefWhenNoNewTag(t *testing.T) {
	candidates := []updateCandidate{gitCandidate("alpha", "acme/skills", "v1.0.0")}

	var stats updateStats
	groups := planUpdates(candidates, func(updateCandidate) (bool, string, error) { return false, "", nil }, &stats)

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].source != "acme/skills#v1.0.0" {
		t.Errorf("source = %q, want %q", groups[0].source, "acme/skills#v1.0.0")
	}
}

// The global lock records the installable name separately from the lock key; the
// name handed to `--skill` must be the former.
func TestCandidateFilterNameDrivesSkillFlag(t *testing.T) {
	candidates := []updateCandidate{{
		lockName:   "my-skill",
		filterName: "My Skill",
		source:     "acme/skills",
		sourceType: string(source.SourceTypeGitHub),
		ref:        "v1.0.0",
	}}

	var stats updateStats
	groups := planUpdates(candidates, upgradeTo("v1.1.0"), &stats)

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if !reflect.DeepEqual(groups[0].skills, []string{"My Skill"}) {
		t.Errorf("skills = %v, want [\"My Skill\"]", groups[0].skills)
	}
	if !reflect.DeepEqual(groups[0].names, []string{"my-skill"}) {
		t.Errorf("names = %v, want [\"my-skill\"]", groups[0].names)
	}
}

// ── Remote tag cache ───────────────────────────────────────────────────────────

func TestRemoteTagCacheServesRepeatLookups(t *testing.T) {
	c := newRemoteTagCache()
	c.entries["acme/skills"] = remoteTagResult{tags: []string{"v1.0.0", "v1.1.0"}}

	// A second lookup must be served from the cache. If it fell through to git,
	// the bogus URL would produce an error instead.
	for i := 0; i < 3; i++ {
		tags, err := c.fetch("acme/skills")
		if err != nil {
			t.Fatalf("fetch %d returned error: %v", i, err)
		}
		if len(tags) != 2 {
			t.Fatalf("fetch %d returned %d tags, want 2", i, len(tags))
		}
	}
}

// Caching failures matters as much as caching successes: an unreachable remote
// must not be retried once per skill.
func TestRemoteTagCacheServesCachedFailure(t *testing.T) {
	c := newRemoteTagCache()
	sentinel := errors.New("ls-remote failed")
	c.entries["acme/skills"] = remoteTagResult{err: sentinel}

	if _, err := c.fetch("acme/skills"); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the cached sentinel", err)
	}
}

func TestCheckRemoteTagUpToDateUsesCache(t *testing.T) {
	c := newRemoteTagCache()
	c.entries["https://github.com/acme/skills"] = remoteTagResult{
		tags: []string{"v1.0.0", "v1.2.0", "v1.3.0-rc.1"},
	}

	upToDate, newRef, err := checkRemoteTagUpToDate("https://github.com/acme/skills", "v1.0.0", c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if upToDate {
		t.Error("upToDate = true, want false")
	}
	// Pre-releases are not upgrade targets.
	if newRef != "v1.2.0" {
		t.Errorf("newRef = %q, want %q", newRef, "v1.2.0")
	}

	upToDate, _, err = checkRemoteTagUpToDate("https://github.com/acme/skills", "v1.2.0", c)
	if err != nil || !upToDate {
		t.Errorf("upToDate = %v, err = %v; want true, nil", upToDate, err)
	}
}

func TestCheckRemoteTagUpToDateRejectsUnpinnedRef(t *testing.T) {
	c := newRemoteTagCache()
	if _, _, err := checkRemoteTagUpToDate("https://github.com/acme/skills", "main", c); err == nil {
		t.Error("expected an error for a non-semver ref, got nil")
	}
	if len(c.entries) != 0 {
		t.Error("an unpinned ref must not trigger a remote lookup")
	}
}

// Two lock entries can resolve to the same installable name; passing it twice
// to `--skill` would inflate the "updated" count without installing anything
// extra.
func TestPlanUpdatesDeduplicatesSkillNamesWithinGroup(t *testing.T) {
	candidates := []updateCandidate{
		{lockName: "alpha", filterName: "shared", source: "acme/skills", sourceType: string(source.SourceTypeGitHub), ref: "v1.0.0"},
		{lockName: "alpha-copy", filterName: "shared", source: "acme/skills", sourceType: string(source.SourceTypeGitHub), ref: "v1.0.0"},
	}

	var stats updateStats
	groups := planUpdates(candidates, upgradeTo("v1.1.0"), &stats)

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if !reflect.DeepEqual(groups[0].skills, []string{"shared"}) {
		t.Errorf("skills = %v, want [\"shared\"]", groups[0].skills)
	}
	if !reflect.DeepEqual(groups[0].names, []string{"alpha", "alpha-copy"}) {
		t.Errorf("names = %v, want both lock names", groups[0].names)
	}
}
