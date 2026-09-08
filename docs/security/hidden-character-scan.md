# Hidden character scan

mdm runs a deterministic local scan before installing skill markdown. The goal is to catch prompt text that is invisible or visually misleading in a normal markdown review.

## Scope

The scan runs on every `.md` file in the selected skill payload:

- `SKILL.md`
- supporting markdown files in the skill directory
- markdown files fetched through the GitHub blob and well-known install paths

The scan is local and does not use an LLM, external service, or non-deterministic heuristic.

## Severity tiers

Findings are tiered by what the codepoint can actually do. Only the blocking
tier gates an install; warnings are printed for the audit trail and never need
`--allow-hidden-chars`. That keeps the flag meaning "I reviewed a real anomaly
and accept it", rather than becoming the reflex answer to every scan failure.

### Blocking

| Category | Codepoints | Reason |
|---|---|---|
| Unicode tags | `U+E0001..U+E007F` | Can smuggle invisible ASCII-like instructions |
| Bidirectional controls | `U+202A..U+202E`, `U+2066..U+2069` | Can make text render in a different order than it is stored (Trojan Source, CVE-2021-42574) |
| Zero-width format chars | `U+200B`, `U+200C`, `U+200D`, `U+200E`, `U+200F`, `U+2060`, `U+FEFF` | Can hide or split instructions in rendered markdown |
| Variation selectors | `U+FE00..U+FE0D`, `U+E0100..U+E01EF`, and `U+FE0E`/`U+FE0F` without a valid emoji base | Can hide extra data in otherwise normal-looking text |
| Soft hyphen | `U+00AD` | Usually invisible unless text wraps |
| Invalid UTF-8 | invalid byte sequences | Markdown should be valid UTF-8 for reliable review |

An initial UTF-8 BOM at the start of a file is allowed. Any later `U+FEFF` is flagged.

### Warning

| Category | Codepoints | Reason |
|---|---|---|
| Emoji variation sequences | `U+FE0E`/`U+FE0F` immediately after a base character it forms a listed sequence with | Ordinary picker emoji such as `⚠️` (`U+26A0 U+FE0F`) or `❤️`. VS15/VS16 can only toggle text-versus-emoji rendering of the single preceding character, so the pair cannot carry hidden content |

"Listed" means the pair appears in the Unicode Consortium's
`emoji-variation-sequences.txt`, which enumerates every valid base and
selector combination. A selector after anything else (`A` + `U+FE0F`, a
selector at the start of a line, or a doubled selector) is genuinely anomalous
and stays in the blocking tier.

The data file is vendored at
`internal/security/markdownscan/unicodedata/emoji-variation-sequences.txt`
and embedded into the binary, so the scan stays offline and reproducible. It is
pinned to a single Unicode version rather than tracking `latest/`; the README
beside it explains how to refresh it, and the package tests fail if the pinned
version and the file's header disagree.

## Install behavior

Every finding is printed with its severity, the skill, file, line, column,
category, and codepoint. If any finding is in the blocking tier, mdm blocks
installation before copying or symlinking files. If every finding is a
warning, mdm prints them and continues.

`--yes` does not bypass a blocking finding. To proceed intentionally, pass `--allow-hidden-chars` to the install command:

```bash
mdm skills add ./my-skill --allow-hidden-chars
mdm skills install -y --allow-hidden-chars
mdm skills sync --allow-hidden-chars
mdm skills update --allow-hidden-chars
```

`--skip-audit` only skips the network security advisory lookup. It does not disable this local scan.

## Out of scope

This scan does not attempt semantic prompt-injection detection, homoglyph scoring, base64 decoding, natural-language classification, or reputation checks. Those checks are intentionally outside v1 so the local install path stays fast, reproducible, and offline.

## Test fixtures

### Local fixtures (no network required)

The repo includes intentionally unsafe fixtures for regression and manual smoke testing:

- `internal/security/markdownscan/testdata/bad-hidden.md` — raw scanner unit-test input
- `internal/security/markdownscan/testdata/emoji.md` — picker emoji that must only warn
- `tests/testdata/hidden-skill/` — a full skill directory with hidden chars in `README.md`
- `tests/testdata/emoji-skill/` — a full skill directory whose `README.md` uses `⚠️`, `❤️`, `✅️`, and `▶️`

Verify install blocking from the repo root:

```bash
mdm skills add ./tests/testdata/hidden-skill --project --agent claude-code -y
```

Expected output: scan failure with file/line/column/codepoint details, installation blocked.

Verify the bypass flag allows installation to proceed:

```bash
mdm skills add ./tests/testdata/hidden-skill --project --agent claude-code -y --allow-hidden-chars
```

Expected output: yellow warnings printed, installation continues.

Verify that ordinary emoji warn without blocking, and without the flag:

```bash
mdm skills add ./tests/testdata/emoji-skill --project --agent claude-code -y
```

Expected output: `variation-selector` warnings naming the emoji base each one completes, followed by a normal install.

### Remote fixture (tests the GitHub clone path)

The `hidden-chars` branch of `https://github.com/sethcarney/custom-plugins.git` contains skills with intentionally malformed hidden characters, exercising the blob/clone install path rather than the local disk path.

Verify blocking via GitHub install:

```bash
mdm skills add https://github.com/sethcarney/custom-plugins.git#hidden-chars
