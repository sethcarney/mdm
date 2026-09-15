package commands

import (
	"strings"
	"testing"
)

// sanitizeName answers every name with no Latin letter or digit in it with
// the one fixed string "unnamed-skill", so two such definitions - a Japanese
// name and a Cyrillic one, say - shared one lock key and one canonical file,
// and installing the second silently replaced the first. The agent disk name
// carries a hash of the raw name instead, and it is stable, because the lock
// key written on add has to find the same file on remove.
//
// Mutation this catches: dropping the hash and returning sanitizeName's
// fallback as it stands.
func TestAgentDiskNameKeepsTwoUnnamedDefinitionsApart(t *testing.T) {
	japanese := agentDiskName("批評家")
	cyrillic := agentDiskName("критик")
	if japanese == cyrillic {
		t.Fatalf("two all-non-Latin names share the disk name %q", japanese)
	}
	if japanese == "unnamed-skill" || cyrillic == "unnamed-skill" {
		t.Errorf("an all-non-Latin name still gets the shared fallback: %q, %q", japanese, cyrillic)
	}
	if again := agentDiskName("批評家"); again != japanese {
		t.Errorf("disk name is not stable across calls: %q then %q", japanese, again)
	}
	for _, name := range []string{japanese, cyrillic} {
		if !strings.HasPrefix(name, "unnamed-") {
			t.Errorf("%q does not say the definition had no usable name", name)
		}
		if agentDiskName(name) != name {
			t.Errorf("agentDiskName(%q) = %q, want it unchanged: the lock key must map onto itself", name, agentDiskName(name))
		}
	}
	// The name the lock holds finds the definition it was made from.
	if !agentNameMatches("批評家", japanese) {
		t.Errorf("the lock key %q does not match the raw name it was made from", japanese)
	}
	if agentNameMatches("критик", japanese) {
		t.Errorf("the lock key %q matches a different raw name", japanese)
	}
}

// sanitizeName caps at 255 bytes, NAME_MAX on most filesystems, but the disk
// name is never the whole file name: Copilot appends ".agent.md", Codex
// ".toml", and the replace-by-rename path reserves a ".mdm-tmp-" sibling. A
// 255-byte name passed the cap and then failed at the first harness whose
// suffix pushed the file name over the limit.
//
// Mutation this catches: removing the agentDiskNameMax cut.
func TestAgentDiskNameLeavesRoomForTheHarnessSuffix(t *testing.T) {
	raw := strings.Repeat("a", 250) + "-----" + strings.Repeat("b", 20)
	name := agentDiskName(raw)
	if len(name) > agentDiskNameMax {
		t.Errorf("disk name is %d bytes, want at most %d", len(name), agentDiskNameMax)
	}
	if len(name)+len(".agent.md")+len(".mdm-tmp-") > 255 {
		t.Errorf("disk name plus the longest suffix and the temp prefix is %d bytes, over NAME_MAX", len(name)+len(".agent.md")+len(".mdm-tmp-"))
	}
	if strings.HasSuffix(name, "-") || strings.HasSuffix(name, ".") {
		t.Errorf("the cut left a trailing separator: %q", name)
	}
	if agentDiskName(name) != name {
		t.Errorf("a capped name does not map onto itself: %q -> %q", name, agentDiskName(name))
	}
	if !agentNameMatches(raw, name) {
		t.Errorf("the capped lock key does not match the raw name it was made from")
	}
	// An ordinary name is untouched by either rule.
	if got := agentDiskName("Code Reviewer"); got != "code-reviewer" {
		t.Errorf("agentDiskName(Code Reviewer) = %q, want code-reviewer", got)
	}
}
