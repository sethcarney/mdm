# Spec: Experimental OKF Knowledge Support (`mdm knowledge`)

| | |
|---|---|
| **Status** | Draft |
| **Stability** | Experimental — gated behind `MDM_EXPERIMENTAL=knowledge` / `mdm experimental enable knowledge` |
| **Author** | Dakota Kim |
| **Created** | 2026-07-06 |
| **Tracking issue** | TBD |
| **External spec** | [Open Knowledge Format v0.1](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf) |

## Summary

Add an experimental `mdm knowledge` command group that installs, validates, and
manages **Open Knowledge Format (OKF) bundles** — directories of markdown files
with YAML frontmatter that give AI agents durable reference context ("LLM-wiki"
pattern). The feature reuses mdm's existing acquisition, locking, and security
pipeline, ships behind a named experimental gate, and makes no stability
promises while OKF itself is at v0.1.

Skills tell an agent **how to do a task**. Knowledge bundles tell an agent
**what is true about a system**. mdm already solves fetch/verify/place/pin/audit
for the first; this spec extends the same pipeline to the second.

## Background

Google published OKF v0.1 in June 2026. A bundle is a directory of markdown
files where each file is one concept, the file path is the concept's identity,
concepts cross-link with ordinary markdown links, and optional `index.md` files
provide hierarchical navigation. Frontmatter requires only `type`; `title`,
`description`, `resource`, `tags`, and `timestamp` are reserved but optional.
Distribution is deliberately plain: "shipped as a tarball, hosted in any repo,
mounted from any filesystem."

This maps almost one-to-one onto what mdm already does for skills:

| Pipeline stage | Existing package | Reuse for OKF |
|---|---|---|
| Parse source (GitHub/GitLab/URL/local) | `internal/source` | As-is |
| Shallow clone / API fetch | `internal/git`, `internal/blob` | As-is |
| Discover markdown artifacts | `internal/skill` | New sibling: `internal/okf` |
| Hidden-character / prompt-smuggling scan | `internal/security` | As-is (higher value here — see below) |
| Pin source+ref+hash in a lock file | `internal/lock` | New file: `knowledge-lock.json` |
| Health checks | `commands/doctor.go` | Extend when gate is enabled |

The genuinely new code is OKF bundle discovery/validation and the experimental
gate itself.

## Goals

1. **Install and manage OKF bundles** from the same source types skills support
   (GitHub, GitLab, direct URL, local path), pinned in a lock file, restorable
   in CI.
2. **Validate bundles** against OKF v0.1: required `type` frontmatter, reserved
   filename rules, cross-link integrity, orphaned-document detection.
3. **Scan bundles for prompt-injection vectors** (hidden characters, smuggled
   instructions) before they land in an agent's context. OKF documents are
   *designed* to be fed to agents and to be written by agents, which makes them
   a first-class injection surface; no other OKF tooling does this today.
4. **Evolve alongside the standard without stability debt**: everything ships
   behind a named experimental gate, records the OKF spec version it targets,
   and is exempt from semver until graduation.
5. **Zero impact when disabled**: users who never enable the gate see no new
   commands, no new files, no behavior change, and cannot have state corrupted
   by the experiment.

## Non-goals (for the experimental phase)

- **Authoring/enrichment pipelines** — generating OKF content from BigQuery,
  databases, or codebases is Google's reference-agent territory, not mdm's.
- **A knowledge registry or search** — there is no skills.sh equivalent for
  OKF yet; `mdm knowledge find` is out of scope until an ecosystem exists.
- **Rendering/visualization** — the OKF repo ships an HTML visualizer; we
  don't compete with it.
- **Automatic agent wiring** — injecting bundle references into
  CLAUDE.md/AGENTS.md is deferred to a later phase (see Future value); it
  needs its own design because it mutates user-owned files.
- **Serving knowledge at runtime** (MCP server, HTTP) — out of scope entirely.

## Value today — an honest assessment

Be clear-eyed about this: **the immediate practical value is modest.**

- OKF is **three weeks old at v0.1**. The known corpus of conformant bundles is
  Google's three samples (GA4, Stack Overflow, Bitcoin). There is no registry,
  no discovery story, and no evidence yet of third-party producers.
- Teams already doing the LLM-wiki pattern do it fine with `git clone` and a
  `docs/` directory. `mdm knowledge add` beats that only via pinning,
  update-checking, and the security scan — real but incremental wins.
- The `audit` verb has nothing external to query; it can only diff local
  hashes against the recorded ref until a registry exists.

What *is* real today:

1. **The validator.** A fast, dependency-free `mdm knowledge validate` (Go
   binary vs. Google's Python tooling) that checks conformance and link
   integrity fills a genuine gap — useful even to people who don't use mdm for
   installation, and cheap to build. This is the highest confidence-to-effort
   item in the whole spec.
2. **The security scan.** Installing agent-bound markdown through a
   hidden-character/prompt-smuggling scanner is a concrete, differentiated
   safety win the moment anyone installs a bundle they didn't author.
3. **Positioning and learning.** Being early to skills worked for mdm. If
   knowledge-as-context keeps rising, the cheapest way to have an informed
   opinion — and influence on the spec — is a working implementation we
   dogfood ourselves.

If OKF stalls, the sunk cost is bounded (see Exit criteria).

## Value later — if the pattern holds

- **Registry integration**: when an OKF registry or `.well-known/knowledge`
  convention appears (mdm already speaks `.well-known/agent-skills` via
  `internal/registry`), `find`/`audit` light up with real data.
- **Agent wiring**: `mdm knowledge add` appending a managed pointer block to
  AGENTS.md/CLAUDE.md (reusing `mdm rules` machinery) turns installed bundles
  from files-on-disk into context agents actually load. This is likely the
  step that makes the feature sticky.
- **Team onboarding**: `mdm knowledge install` restoring an org's pinned
  knowledge bundles in CI/onboarding, exactly like `mdm skills install`.
- **Cross-format hedge**: if a competing knowledge format emerges, the
  acquisition/lock/scan pipeline is format-agnostic; only discovery/validation
  is OKF-specific (one module, not a rewrite).
- **Doctor as knowledge CI**: link-rot and staleness checks (`timestamp` drift)
  over a team's own bundle, run in CI — mdm becomes the lint step for the
  knowledge itself, not just the installer.

## Design

### The experimental gate

A named-feature gate (kubectl-alpha / GOEXPERIMENT precedent), not a single
boolean, so future experiments reuse the rail.

**`internal/experimental/`** (new, small):

```go
// Feature is a named experimental capability.
type Feature string

const Knowledge Feature = "knowledge"

// Enabled reports whether f is active via the MDM_EXPERIMENTAL env var
// (comma-separated names, or "all") or a persisted opt-in.
func Enabled(f Feature) bool
```

**Activation paths:**

1. `MDM_EXPERIMENTAL=knowledge` — ephemeral, CI-friendly, no state.
2. `mdm experimental enable knowledge` — persists to an
   `Experimental []string` field on the existing global lock file
   (`SkillLockFile`), alongside the precedent set by `ConfiguredAgents` and
   `DismissedPrompts`. Losing this field to an older binary's rewrite is
   acceptable — it's a toggle, not data.

**`mdm experimental` command group** (always visible):

```
mdm experimental
├── list                 # available features, status, one-line description + spec link
├── enable <feature>
└── disable <feature>
```

**Registration behavior** (`BuildRootCmd`):

- The `knowledge` command is always registered.
- **Disabled**: `Hidden: true` (absent from help and shell completion) plus a
  `PersistentPreRunE` that refuses with a pointer:
  `knowledge is experimental — enable with 'mdm experimental enable knowledge' or MDM_EXPERIMENTAL=knowledge`.
  Discoverable for people who heard about it; invisible to everyone else.
- **Enabled**: visible with an `[experimental]` suffix in help text, and every
  invocation prints one dim banner line:
  `⚠ experimental: OKF support tracks spec v0.1 — commands and file formats may change`.

**Stability contract**: README and release notes state that experimental
features may change or be removed in any release, exempt from semver.

### Command surface (behind the gate)

```
mdm knowledge
├── add <source>         # install a bundle from GitHub, GitLab, URL, or local path
│     --dir <path>       #   install root (default ./knowledge/)
│     --ref <ref>        #   branch/tag/sha, as skills add
│     --dry-run
├── list                 # installed bundles: name, source, ref, doc count, spec version
├── remove [bundles...]
├── update [bundles...]  # re-fetch from recorded source+ref; re-scan; re-validate
├── validate [path]      # OKF conformance check on an installed bundle or arbitrary dir
├── init [name]          # scaffold a minimal conformant bundle (index.md + one concept)
└── install              # restore all bundles from knowledge-lock.json (CI/onboarding)
```

Verbs, aliases, and flag conventions mirror `mdm skills` exactly — the group
should feel like the same tool, not a bolted-on subproject.

**Install destination.** Unlike skills, no per-agent knowledge directory
convention exists. Default: `./knowledge/<bundle-name>/` at the project root,
overridable with `--dir`. Project-scoped only for the experimental phase; a
global scope can follow if a use case appears.

### Lock file: separate by design

Knowledge entries live in **`knowledge-lock.json`**, *not* in
`skills-lock.json`.

Rationale: `internal/lock` reads by unmarshaling into a fixed struct and
**rewrites the file wholesale** on every change. Any older mdm binary (or a
teammate on stable) touching skills would silently drop unknown keys — so
experimental data in the shared file would be corruptible by non-experimental
usage. A separate file gives the experiment zero blast radius and makes
eventual graduation an explicit, versioned migration instead of an accretion.

```json
{
  "version": 1,
  "bundles": {
    "acme-sales": {
      "source": "github.com/acme/sales-knowledge",
      "sourceType": "github",
      "ref": "v2.1.0",
      "installDir": "knowledge/acme-sales",
      "specVersion": "0.1",
      "contentHash": "sha256:…",
      "installedAt": "2026-07-06T…",
      "updatedAt": "2026-07-06T…"
    }
  }
}
```

`specVersion` records which OKF revision the bundle conformed to at install
time, so the validator can evolve with the standard and warn on drift.

### `internal/okf/` (new)

The only substantial new domain code:

- **Discovery**: locate bundle roots in a fetched tree (heuristic: directories
  of `.md` files with OKF frontmatter; explicit path wins over heuristics).
- **Parsing**: frontmatter (`type` required; `title`, `description`,
  `resource`, `tags`, `timestamp` reserved) — extend or mirror
  `internal/skill`'s frontmatter handling rather than duplicating it.
- **Validation**: per-document conformance, reserved-filename rules
  (`index.md`, `log.md`), cross-link resolution (every relative markdown link
  resolves inside the bundle), orphan detection (documents unreachable from
  any `index.md`). Output modes: human (ANSI, like doctor) and `--json` for CI.

Validation rules are versioned against the pinned SPEC.md revision; when the
spec moves, rules update behind the gate with no compatibility burden.

### Security scan is mandatory, not optional

`add` and `update` run `internal/security`'s markdownscan over every document
before copying, exactly as skills installs do (`commands/hidden_scan.go`).
Findings block the install pending explicit confirmation. This is a headline
behavior of the feature, not an implementation detail.

### Doctor integration

When (and only when) the gate is enabled, `mdm doctor` adds a knowledge
section: lock-vs-disk hash drift, broken cross-links, missing `type` fields,
stale `timestamp`s. When disabled, doctor's output is byte-identical to today.

## Build plan

Each PR lands green on the full pre-PR checklist (`gofmt -s`, `go test ./...`,
`govulncheck`, `gocyclo -over 16`) and leaves `main` shippable — the gate is
what makes incremental merging safe.

| PR | Branch | Contents |
|---|---|---|
| 1 | `feat/experimental-gate` | `internal/experimental/`, `mdm experimental` group, `Experimental` field on `SkillLockFile`, hidden `knowledge` skeleton that refuses when disabled. Docs: `docs/experimental.md`. |
| 2 | `feat/okf-validate` | `internal/okf/` parsing + validation, `mdm knowledge validate`, `init`. Pure-local, no network — the highest-value, lowest-risk slice ships first. |
| 3 | `feat/knowledge-add` | `add`/`list`/`remove` wired through `source`/`git`/`blob` + security scan + `knowledge-lock.json`. |
| 4 | `feat/knowledge-update` | `update`, `install`, doctor integration. |

Defer (tracked, not built): agent wiring into AGENTS.md, registry integration,
audit against external data, global scope.

## Testing strategy

Follow the repo's existing two-tier pattern.

**Unit tests** (alongside packages):

- `internal/experimental`: env-var parsing (single, list, `all`, empty),
  persisted-toggle read, precedence.
- `internal/okf`: table-driven over `testdata/` fixture bundles —
  a conformant bundle, missing `type`, broken cross-link, orphaned doc,
  reserved-filename violation, non-OKF markdown dir (discovery must decline).
  Google's sample bundles (GA4 et al.) vendored as a conformance fixture so
  spec drift shows up as a test failure.
- `internal/lock`: knowledge lock round-trip; assert `skills-lock.json` is
  untouched by knowledge operations.

**End-to-end tests** (`tests/`, same harness as `cli_test.go` — build the real
binary, exec it):

- **Gate off** (default): `knowledge` absent from `mdm --help` and completion
  output; invoking it exits non-zero with the enable hint; `mdm doctor` output
  unchanged. These are the regression tests that keep the "graceful addition"
  promise.
- **Gate on** (`MDM_EXPERIMENTAL=knowledge` in the test env): `add` from a
  local-path fixture bundle → files land under `knowledge/`, lock written,
  banner printed; `validate` passes/fails the right fixtures with the right
  exit codes; `remove` cleans files + lock; `install` restores from lock into
  a fresh temp dir.
- **Security**: a fixture bundle with hidden characters (mirroring
  `tests/testdata/hidden-skill`) blocks the install.
- **Cross-binary safety**: after a knowledge install, run a skills operation
  and assert `knowledge-lock.json` survives byte-identical.

**Not covered / accepted risk**: live GitHub/GitLab fetches (network) are
exercised the same way skills' are today — via the shared `source`/`git` paths
already under test; e2e fixtures use local paths.

## Graduation criteria

Promote out of experimental (visible by default, semver-covered, lock
migration into the mainline) when all of:

1. OKF publishes a stabilized revision (v1.0 or explicit stability statement),
   or 6+ months pass with the spec stable in practice.
2. Demonstrated third-party demand: non-trivial external usage, issues, or
   conformant bundles in the wild that aren't Google's samples.
3. The command surface has survived a full release cycle without breaking
   changes.
4. The install-destination and agent-wiring questions have settled answers.

## Exit criteria (kill switch)

If after ~2 quarters OKF shows no ecosystem traction, or a competing format
clearly wins: delete `commands/knowledge.go`, `internal/okf/`, and the feature
registry entry; keep `internal/experimental` (reusable rail) and the validator
learnings. Because nothing was stable, removal is a minor release and a
changelog line. Users' installed bundle *files* are never deleted by removal —
only the tooling goes.

## Risks

| Risk | Mitigation |
|---|---|
| OKF spec churns under us | Gate + `specVersion` pinning + versioned validator; we track, nothing breaks users |
| OKF loses to a competing format | Pipeline is format-agnostic; only `internal/okf` is specific; exit criteria bound the cost |
| Scope creep toward "knowledge platform" | Non-goals above; deferred list is tracked, not built |
| Confusing skills vs. knowledge for users | Distinct noun, distinct docs page, help-text one-liners stating the difference |
| Experimental state corrupted by stable binaries | Separate `knowledge-lock.json`; cross-binary safety e2e test |

## Open questions

1. **Bundle naming/identity**: derive from repo name, top-level `index.md`
   title, or directory name? (Lean: directory name, sanitized, `--name`
   override — matches skills.)
2. **Multiple bundles per repo**: support `--bundle` filtering like `--skill`,
   or one-bundle-per-add for v1? (Lean: discover all, prompt — matches skills
   UX.)
3. **Where validate's spec text lives**: vendor OKF's SPEC.md rules as code
   with a pinned upstream commit hash, or fetch at runtime? (Lean: vendor;
   `validate` must work offline.)
