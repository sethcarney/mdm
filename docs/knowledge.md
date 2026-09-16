# Knowledge bundles

`mdm knowledge` installs, validates, and updates [Open Knowledge Format
(OKF)](https://github.com/GoogleCloudPlatform/open-knowledge-format) bundles -
portable, versioned documentation packages an AI harness can read. Bundles are
installed into `./knowledge/` in your project and recorded in the `knowledge`
section of `mdm.lock`, so a teammate restores them with one command.

`mdm knowledge` is a stable, first-class command group. It was introduced behind
an experimental gate in an earlier build; that gate was removed in v2, and the
separate `knowledge-lock.json` is now the `knowledge` section of `mdm.lock` (run
`mdm migrate` to fold an old file in). See [the design spec](specs/knowledge.md)
for the history and the conformance rules.

## Commands

| Command | What it does |
| --- | --- |
| `mdm knowledge add <source>` | Install a bundle from GitHub, a URL, or a local path into `./knowledge/` and record it in `mdm.lock`. |
| `mdm knowledge list` | List installed bundles (alias: `ls`). |
| `mdm knowledge update [bundles...]` | Re-fetch bundles from their recorded source and ref. |
| `mdm knowledge remove [bundles...]` | Remove bundles and their lock entries (aliases: `rm`, `r`). |
| `mdm knowledge validate [path]` | Check OKF conformance and link integrity (`--json` for machine output). |
| `mdm knowledge init [name]` | Scaffold a minimal conformant bundle. |
| `mdm knowledge install` | Restore every bundle from `mdm.lock` - the CI / onboarding path. |

## Typical flow

```bash
# Author or install a bundle
mdm knowledge init my-bundle       # scaffold one
mdm knowledge add owner/repo       # or install an existing one

# Check it, then commit mdm.lock
mdm knowledge validate ./knowledge/my-bundle
git add mdm.lock knowledge/

# A teammate restores everything on a fresh clone
mdm knowledge install
```

Every install runs the same hidden-character scan as skills; pass
`--allow-hidden-chars` to override it deliberately. `mdm doctor` reports on
installed bundles alongside skills and agent definitions.
