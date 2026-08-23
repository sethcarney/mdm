package lock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// ──────────────────────────────────────────────────────────
// Knowledge bundle lock — the knowledge section of mdm-lock.json
//
// v1 kept knowledge entries in their own knowledge-lock.json so a stable
// binary rewriting the skills lock could not drop them. In v2 the unified
// mdm-lock.json preserves unknown top-level keys on every write (see
// project.go), so the sections share one file safely.
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

// KnowledgeLockFile is a view of the knowledge section of the project lock.
type KnowledgeLockFile struct {
	Version int                           `json:"version"`
	Bundles map[string]KnowledgeLockEntry `json:"bundles"`
}

// readLegacyKnowledgeLockE reads the v1 knowledge-lock.json directly. It is
// only consulted when mdm-lock.json does not exist. Corrupt or
// newer-versioned files are an error, matching the final v1 patch releases.
func readLegacyKnowledgeLockE(cwd string) (KnowledgeLockFile, error) {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	path := filepath.Join(cwd, "knowledge-lock.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return EmptyKnowledgeLock(), nil
		}
		return EmptyKnowledgeLock(), errUnreadableLock(path, err)
	}
	var lk KnowledgeLockFile
	if err := json.Unmarshal(data, &lk); err != nil {
		return EmptyKnowledgeLock(), errUnreadableLock(path, err)
	}
	if lk.Version > knowledgeLockVersion {
		return EmptyKnowledgeLock(), errNewerLock(path, lk.Version, knowledgeLockVersion)
	}
	if lk.Bundles == nil || lk.Version < knowledgeLockVersion {
		return EmptyKnowledgeLock(), nil
	}
	return lk, nil
}

func ReadKnowledgeLock(cwd string) KnowledgeLockFile {
	return KnowledgeLockFile{Version: knowledgeLockVersion, Bundles: ReadProjectLock(cwd).Knowledge}
}

func WriteKnowledgeLock(lk KnowledgeLockFile, cwd string) error {
	pl := ReadProjectLock(cwd)
	pl.Knowledge = lk.Bundles
	return WriteProjectLock(pl, cwd)
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
	return WriteKnowledgeLock(lk, cwd)
}
