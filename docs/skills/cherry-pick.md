# mdm skills cherry-pick

Fork third-party skills into your own project, so you can edit them and ship them as yours.

## Usage

```
mdm skills cherry-pick <source>
```

`<source>` accepts the same forms as [`mdm skills add`](add.md) — GitHub shorthand, full GitHub/GitLab/Bitbucket URLs, any git URL with an optional `#ref`, and local paths — with one exception: well-known registry aliases serve files without a repository to fork from, so install those with `mdm skills add` first and cherry-pick the installed copy.

## How it differs from `skills add`

|  | `mdm skills add` | `mdm skills cherry-pick` |
| --- | --- | --- |
| Where the skill lands | each agent's skills directory | `./skills/<name>` — your source tree |
| Who owns it afterwards | upstream | you |
| `mdm skills update` | re-fetches and replaces it | leaves it alone |
| Provenance | `skills-lock.json` entry | `.mdm-origin.json` + `ATTRIBUTION.md` inside the fork |

`add` keeps a skill in sync with its author. `cherry-pick` deliberately breaks that link: the copy becomes a file in your repository like any other, and divergence from upstream is the point.

## Fork flow

1. The source is fetched (shallow clone, or read in place for a local path).
2. `SKILL.md` files are discovered; a picker lets you choose which ones to fork.
3. Markdown files are scanned for hidden Unicode characters.
4. Licensing is resolved — see [Licensing](#licensing) below — and you are asked to confirm if a source declares no terms at all.
5. Each skill directory is copied to `./skills/<name>`, renamed if `--as` was given.
6. Provenance is written into the fork: `.mdm-origin.json`, `ATTRIBUTION.md`, and the upstream license file when the skill directory did not already carry one.
7. With `--install`, the forks are installed into your agents from your copy — recorded in `skills-lock.json` as a local source, which `mdm skills update` skips.

## Flags

| Flag | Description |
| --- | --- |
| `--dir`, `-d` | Directory to fork into, relative to the project root (default `skills`) |
| `--skill`, `-s` | Skill names to fork (repeatable; use `*` for all) |
| `--as` | Rename the forked skill (single skill only) |
| `--install`, `-i` | Also install the forks into your agents' skills directories |
| `--agent`, `-a` | Agents to install the forks to (repeatable; implies `--install`) |
| `--global`, `-g` / `--project`, `-p` | Install scope, with `--install` |
| `--copy` | Copy instead of symlinking, with `--install` |
| `--yes`, `-y` | Skip confirmation prompts |
| `--force` | Replace an existing fork, discarding local edits |
| `--dry-run` | Show what would be forked without writing anything |
| `--list`, `-l` | List the skills available at the source without forking |
| `--status` | Show this project's forks and whether they have been edited |
| `--full-depth` | Search all subdirectories for `SKILL.md` files |
| `--allow-hidden-chars` | Allow markdown files with hidden Unicode characters |
| `--no-attribution` | Do not write `ATTRIBUTION.md` |

## Examples

```bash
# Pick from a repo interactively
mdm skills cherry-pick vercel-labs/agent-skills

# Take one skill at a pinned tag, under your own name
mdm skills cherry-pick owner/repo#v1.2.0 -s code-review --as our-code-review

# Fork a skill you already installed, then keep editing it
mdm skills cherry-pick ./.agents/skills/code-review

# Fork and wire it straight into Claude Code
mdm skills cherry-pick owner/repo -s code-review --install -a claude-code

# What have we forked, and have we changed it?
mdm skills cherry-pick --status
```

## What lands in the fork

```
skills/our-code-review/
├── SKILL.md            # renamed by --as; otherwise byte-for-byte upstream
├── references/…        # everything else the skill shipped
├── ATTRIBUTION.md      # human-readable notice — travels with the skill when installed
├── LICENSE.upstream    # upstream license text, when the skill dir didn't carry its own
└── .mdm-origin.json    # machine-readable provenance
```

`.mdm-origin.json` records the upstream name, source, ref, commit, path, license, and a content hash taken at the fork point:

```json
{
  "version": 1,
  "skill": "our-code-review",
  "upstream": {
    "name": "code-review",
    "source": "owner/repo",
    "sourceType": "github",
    "sourceUrl": "https://github.com/owner/repo",
    "ref": "v1.2.0",
    "commit": "b8caa260a420a73042e35521de4b5c8baf6446cc",
    "skillPath": "skills/code-review/SKILL.md",
    "license": "MIT",
    "licenseFile": "LICENSE.upstream"
  },
  "contentHash": "sha256:…",
  "forkedAt": "2026-02-01T09:12:44Z"
}
```

The hash is what `--status` compares against to tell an untouched fork from one you have started building on. The dot prefix keeps the file out of installed copies — `copyDirectory` skips dotfiles — while `ATTRIBUTION.md` is not dot-prefixed on purpose, so the notice follows the material into every agent's skills directory.

When you fork a skill mdm installed earlier, the lock file still knows where it really came from, so the record credits the original repository and notes the local path it was copied through as `via`.

## Licensing

A fork redistributes someone else's work, and mdm cannot make that lawful for you. What it does is make the position visible:

- **Terms are looked up** in the skill directory first, then the repository root, across the usual `LICENSE`/`LICENCE`/`COPYING`/`UNLICENSE` spellings. A `license:` field in the skill's frontmatter is taken at its word.
- **The license text is copied into the fork** as `LICENSE.upstream` when the skill directory did not already ship one, because most permissive licenses require the notice to travel with the copy.
- **A source with no license at all stops the command** and asks for confirmation. Absent a license the default is that no redistribution rights are granted — a fork of it may not be yours to publish, and `ATTRIBUTION.md` says so.

Honouring the terms — attribution, share-alike, or simply asking first — remains yours to do. `--no-attribution` omits the generated notice; it does not omit the obligation.

## The forks directory and OpenClaw

`./skills` is also OpenClaw's project skills directory, which is what makes it a
publishable location — but it means agent-level commands can reach your forks
through it. mdm treats a directory carrying `.mdm-origin.json` as your source
code rather than an install, so:

- `mdm skills remove` uninstalls the skill from your agents and leaves the fork
  in `./skills` alone.
- `mdm agents remove openclaw` cleans the directory but keeps the forks in it,
  reporting how many it kept.
- `--install` skips any agent that reads the forks directory directly — the fork
  is already where that agent looks, and installing would replace it with a
  symlink to a copy of itself.

Hand-written skills in `./skills` that were never cherry-picked carry no origin
file and are **not** covered by these guards — `mdm agents remove openclaw` still
deletes them (see [Troubleshooting](../troubleshooting.md)). Pass `--dir` to keep forks somewhere
else if you would rather not share the directory at all.

## Updating a fork

Forks never update themselves; that is the deal. To take a newer upstream version:

```bash
# Replace the fork wholesale, losing local edits
mdm skills cherry-pick owner/repo#v2.0.0 -s code-review --as our-code-review --force

# Or fork the new version alongside the old one and merge by hand
mdm skills cherry-pick owner/repo#v2.0.0 -s code-review --as code-review-v2
```

`--status` marks a fork as *edited since fork* precisely so you know which of those two you want. Because the fork lives in your repository, `git diff` is the other half of the answer.
