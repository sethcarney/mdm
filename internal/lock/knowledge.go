package lock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ──────────────────────────────────────────────────────────
// Knowledge bundle lock (knowledge-lock.json, project scope)
//
// Knowledge entries deliberately live in their own file rather than in
// skills-lock.json: the skill locks are read into fixed structs and
// rewritten wholesale, so an older mdm binary touching skills would
// silently drop unknown keys. A separate file keeps the experimental
// knowledge feature invisible to — and incorruptible by — stable binaries.
// ──────────────────────────────────────────────────────────

const knowledgeLockVersion = 1

type KnowledgeLockEntry struct {
	Source      string `json:"source"`
	SourceType  string `json:"sourceType"`
	SourceURL   string `json:"sourceUrl,omitempty"`
	Ref         string `json:"ref,omitempty"`
	Subpath     string `json:"subpath,omitempty"`
	InstallDir  string `json:"installDir"` // slash-separated, relative to the project root
	SpecVersion string `json:"specVersion"`
	ContentHash string `json:"contentHash,omitempty"`
	InstalledAt string `json:"installedAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type KnowledgeLockFile struct {
	Version int                           `json:"version"`
	Bundles map[string]KnowledgeLockEntry `json:"bundles"`
}

func GetKnowledgeLockPath(cwd string) string {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	return filepath.Join(cwd, "knowledge-lock.json")
}

func ReadKnowledgeLock(cwd string) KnowledgeLockFile {
	data, err := os.ReadFile(GetKnowledgeLockPath(cwd))
	if err != nil {
		return EmptyKnowledgeLock()
	}
	var lk KnowledgeLockFile
	if err := json.Unmarshal(data, &lk); err != nil {
		return EmptyKnowledgeLock()
	}
	if lk.Bundles == nil || lk.Version < knowledgeLockVersion {
		return EmptyKnowledgeLock()
	}
	return lk
}

func WriteKnowledgeLock(lk KnowledgeLockFile, cwd string) error {
	// Sort keys for deterministic output
	sorted := KnowledgeLockFile{Version: lk.Version, Bundles: map[string]KnowledgeLockEntry{}}
	keys := make([]string, 0, len(lk.Bundles))
	for k := range lk.Bundles {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sorted.Bundles[k] = lk.Bundles[k]
	}
	data, err := json.MarshalIndent(sorted, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(GetKnowledgeLockPath(cwd), append(data, '\n'), 0600)
}

func EmptyKnowledgeLock() KnowledgeLockFile {
	return KnowledgeLockFile{Version: knowledgeLockVersion, Bundles: map[string]KnowledgeLockEntry{}}
}

func AddBundleToKnowledgeLock(name string, entry KnowledgeLockEntry, cwd string) error {
	lk := ReadKnowledgeLock(cwd)
	now := time.Now().UTC().Format(time.RFC3339)
	if existing, ok := lk.Bundles[name]; ok {
		entry.InstalledAt = existing.InstalledAt
	} else {
		entry.InstalledAt = now
	}
	entry.UpdatedAt = now
	lk.Bundles[name] = entry
	return WriteKnowledgeLock(lk, cwd)
}

func RemoveBundleFromKnowledgeLock(name, cwd string) error {
	lk := ReadKnowledgeLock(cwd)
	if _, ok := lk.Bundles[name]; !ok {
		return nil
	}
	delete(lk.Bundles, name)
	if len(lk.Bundles) == 0 {
		return os.Remove(GetKnowledgeLockPath(cwd))
	}
	return WriteKnowledgeLock(lk, cwd)
}
