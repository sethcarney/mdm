# Hidden character scan

mdm runs a deterministic local scan before installing skill markdown. The goal is to catch prompt text that is invisible or visually misleading in a normal markdown review.

## Scope

The scan runs on every `.md` file in the selected skill payload:

- `SKILL.md`
- supporting markdown files in the skill directory
- markdown files fetched through the GitHub blob and well-known install paths

The scan is local and does not use an LLM, external service, or non-deterministic heuristic.

## Blocked characters

| Category | Codepoints | Reason |
|---|---|---|
| Unicode tags | `U+E0001..U+E007F` | Can smuggle invisible ASCII-like instructions |
| Bidirectional controls | `U+202A..U+202E`, `U+2066..U+2069` | Can make text render in a different order than it is stored |
| Zero-width format chars | `U+200B`, `U+200C`, `U+200D`, `U+200E`, `U+200F`, `U+2060`, `U+FEFF` | Can hide or split instructions in rendered markdown |
| Variation selectors | `U+FE00..U+FE0F`, `U+E0100..U+E01EF` | Can hide extra data in otherwise normal-looking text |
| Soft hyphen | `U+00AD` | Usually invisible unless text wraps |
| Invalid UTF-8 | invalid byte sequences | Markdown should be valid UTF-8 for reliable review |

An initial UTF-8 BOM at the start of a file is allowed. Any later `U+FEFF` is flagged.

## Install behavior

If findings are present, mdm prints the skill, file, line, column, category, and codepoint, then blocks installation before copying or symlinking files.

`--yes` does not bypass this check. To proceed intentionally, pass `--allow-hidden-chars` to the install command:

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

The repo includes intentionally unsafe fixtures for regression and manual smoke testing:

- `internal/security/markdownscan/testdata/bad-hidden.md`
- `tests/testdata/hidden-skill/`

To manually verify install blocking from the repo root:

```bash
mdm skills add ./tests/testdata/hidden-skill --project --agent claude-code -y
```
