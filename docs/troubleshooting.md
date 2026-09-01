# Troubleshooting

Known sharp edges, what causes them, and how to get out of them.

## "mdm.lock was written by a newer version of mdm"

**Symptom.** Any command exits with
`mdm.lock was written by a newer version of mdm (lock version N ...)` —
or, from a patched v1 binary, the same error naming `skills-lock.json`
(v2's migration tombstone carries a version v1 refuses on purpose).

**Cause.** Someone on the project — or the project's CI image — upgraded mdm
across a major version and migrated the lock files to a newer format. This
binary cannot read that format, and it refuses to guess: older releases used
to treat an unreadable lock as *empty*, which made `mdm skills install` in a
migrated project a silent no-op — exit 0, nothing installed. Failing loudly
is the fix for that, so this error is working as intended.

**Recovery.** Run `mdm upgrade` (or update the pinned version in your CI
image / dev container) and re-run the command. Nothing on disk is touched
before the error: read *and* write paths both abort, so an old binary can
never overwrite a newer lock file.

## "mdm.lock could not be parsed"

**Symptom.** Any command exits with `... could not be parsed` and a
JSON error.

**Cause.** The lock file is not valid JSON — usually a bad merge-conflict
resolution. mdm refuses to proceed rather than treat a damaged committed
file as "no skills installed".

**Recovery.** Fix the JSON by hand, or restore the file from version
control (`git restore mdm.lock`), then re-run.

## `mdm agents remove` deleted skills I wrote by hand

**Symptom.** You keep your own skills in `./skills/`, run `mdm agents remove openclaw`,
and the whole directory is gone.

**Cause.** Removing an agent cleans up the files that belong exclusively to it,
including its skills directory. OpenClaw's project skills directory is literally
`skills/` — the same conventional location many projects use for hand-written
skills — so mdm cannot tell your work from OpenClaw's install and removes the
directory whole.

This affects one agent in practice. Of the 31 agents that own a project skills
directory, OpenClaw is the only one whose directory is not dot-prefixed:

| Agent | Project skills directory | Removed by `mdm agents remove <agent>` |
| --- | --- | --- |
| OpenClaw | `skills/` | yes — **and this is where people keep their own skills** |
| Claude Code, Roo, Windsurf, Goose, and 26 others | `.claude/skills/`, `.roo/skills/`, … | yes, but the directory is unambiguously the agent's |
| Cursor, Codex, Gemini CLI, and 11 others | shared `.agents/skills/` | never — shared directories are always preserved |

**Recovery.** If the directory was committed, `git restore skills/` (or
`git checkout -- skills/`) brings it back. If it was untracked and uncommitted,
mdm deletes it outright and there is nothing to restore — the removal does not
go through the trash.

**Avoiding it.**

- Commit `./skills/` before running `mdm agents remove`. This is the reliable
  protection, and it is worth doing anyway for anything you have written.
- Keep hand-written skills somewhere OpenClaw does not claim — any directory
  that is not `./skills/` is untouched by agent removal.
- Or do not configure OpenClaw as an agent. `mdm agents list` shows what is
  configured; only configured agents are cleaned up.

**Cherry-picked skills are already protected.** A skill forked with
[`mdm skills cherry-pick`](skills/cherry-pick.md) carries an `.mdm-origin.json`
file, and mdm treats any directory carrying one as your source code rather than
an install: `mdm agents remove` cleans around it and reports how many it kept,
and `mdm skills remove` leaves it alone. Hand-written skills have no such marker,
which is why they are still at risk.

## My copied skills came back as symlinks

**Symptom.** You installed with `--copy` expecting real directories, but
after `mdm install` (or `mdm skills update`) they come back as symlinks.

**Cause.** Versions before the install-mode switch did not record how a
project was installed, so restores and updates always defaulted to
symlinks, regardless of how the skill first arrived.

**Recovery.** Run `mdm migrate`. It inspects each configured agent's
install directory, or every agent the scope supports when the lock records
no agents, and records `installMode: copy` when it finds real skill
directories there instead of symlinks: in `mdm.lock` for the project, and
in `mdm-state.json` for skills you installed with `-g`. A directory only
counts as a copied skill when it holds a `SKILL.md`, so an unrelated
directory that happens to share a skill's name is ignored. Later installs
and updates copy from then on. Use `mdm migrate --dry-run` first to see the
mode it would record.

`mdm doctor` tells you when this is pending: it reports a scope whose
skills are copied but whose lock does not record it, and points at
`mdm migrate`. Run it afterward too, to confirm the skills are otherwise
healthy.

## A forked skill vanished after installing it to OpenClaw

Installing a skill replaces the agent's `<skills dir>/<name>`, and for OpenClaw
that path is inside the forks directory itself — so installing a fork there would
overwrite the fork with a symlink to a copy of itself.

`mdm skills cherry-pick --install` detects this and skips the agent, printing:

```
• OpenClaw already reads ./skills — skipping its install so the fork is not overwritten
```

That is not an error. The fork is already sitting where OpenClaw looks for
skills, so no install is needed. If you see the fork replaced by a symlink, you
are on a build from before this guard — upgrade with `mdm upgrade`.
