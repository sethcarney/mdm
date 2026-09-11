# Troubleshooting

Known sharp edges, what causes them, and how to get out of them.

## "mdm.lock was written by a newer version of mdm"

**Symptom.** Any command exits with
`mdm.lock was written by a newer version of mdm (lock version N ...)` -
or, from a patched v1 binary, the same error naming `skills-lock.json`
(v2's migration tombstone carries a version v1 refuses on purpose).

**Cause.** Someone on the project - or the project's CI image - upgraded mdm
across a major version and migrated the lock files to a newer format. This
binary cannot read that format, and it refuses to guess: older releases used
to treat an unreadable lock as *empty*, which made `mdm skills install` in a
migrated project a silent no-op - exit 0, nothing installed. Failing loudly
is the fix for that, so this error is working as intended.

**Recovery.** Run `mdm upgrade` (or update the pinned version in your CI
image / dev container) and re-run the command. Nothing on disk is touched
before the error: read *and* write paths both abort, so an old binary can
never overwrite a newer lock file.

## "mdm.lock could not be parsed"

**Symptom.** Any command exits with `... could not be parsed` and a
JSON error.

**Cause.** The lock file is not valid JSON - usually a bad merge-conflict
resolution. mdm refuses to proceed rather than treat a damaged committed
file as "no skills installed".

**Recovery.** Fix the JSON by hand, or restore the file from version
control (`git restore mdm.lock`), then re-run.

## "unknown flag: --agent", or `mdm agents add cursor` tries to clone a repository

This release renamed the AI tool from *agent* to *harness*. `--agent` and `-a`
became `--harness` on the skills, plugins, rules and cherry-pick commands, and
the `mdm agents` command group that managed the configured tool list became
`mdm harnesses`. `mdm agents` now installs [agent definitions](agent-artifacts.md),
so `mdm agents add cursor` reads `cursor` as a source and stops with a message
pointing at `mdm harnesses add cursor`.

Update the invocation:

```bash
mdm skills add ./sk --agent claude-code   # before
mdm skills add ./sk --harness claude-code # now

mdm agents add cursor                     # before
mdm harnesses add cursor                  # now
```

There is deliberately no alias. `mdm agents add <name>` kept working across the
rename would have changed meaning without telling anyone.

## `mdm harnesses remove` deleted skills I wrote by hand

**Symptom.** You keep your own skills in `./skills/`, run `mdm harnesses remove openclaw`,
and the whole directory is gone.

**Cause.** Removing a harness cleans up the files that belong exclusively to it,
including its skills directory. OpenClaw's project skills directory is literally
`skills/` - the same conventional location many projects use for hand-written
skills - so mdm cannot tell your work from OpenClaw's install and removes the
directory whole.

This affects one harness in practice. Of the 31 harnesses that own a project skills
directory, OpenClaw is the only one whose directory is not dot-prefixed:

| Harness | Project skills directory | Removed by `mdm harnesses remove <harness>` |
| --- | --- | --- |
| OpenClaw | `skills/` | yes - **and this is where people keep their own skills** |
| Claude Code, Roo, Windsurf, Goose, and 26 others | `.claude/skills/`, `.roo/skills/`, … | yes, but the directory is unambiguously the harness's |
| Cursor, Codex, Gemini CLI, and 11 others | shared `.agents/skills/` | never - shared directories are always preserved |

**Recovery.** If the directory was committed, `git restore skills/` (or
`git checkout -- skills/`) brings it back. If it was untracked and uncommitted,
mdm deletes it outright and there is nothing to restore - the removal does not
go through the trash.

**Avoiding it.**

- Commit `./skills/` before running `mdm harnesses remove`. This is the reliable
  protection, and it is worth doing anyway for anything you have written.
- Keep hand-written skills somewhere OpenClaw does not claim - any directory
  that is not `./skills/` is untouched by harness removal.
- Or do not configure OpenClaw as a harness. `mdm harnesses list` shows what is
  configured; only configured harnesses are cleaned up.

**Cherry-picked skills are already protected.** A skill forked with
[`mdm skills cherry-pick`](skills/cherry-pick.md) carries an `.mdm-origin.json`
file, and mdm treats any directory carrying one as your source code rather than
an install: `mdm harnesses remove` cleans around it and reports how many it kept,
and `mdm skills remove` leaves it alone. Hand-written skills have no such marker,
which is why they are still at risk.

## `mdm skills remove --harness X` kept the skill instead of deleting it

**Symptom.** You ran `mdm skills remove demo --harness claude-code` expecting the
skill to be gone, and instead saw:

```
! demo: removed from Claude Code, but Roo Code still has it - keeping the skill and its lock entry
```

**Cause.** This is the flag working. `--harness` is a scoped removal: it takes the
named harness's copy and nothing else. Because another harness still has the
skill, the canonical `.agents/skills/demo` directory and the `mdm.lock` entry are
both kept.

They have to be. Every other harness installs by symlinking that canonical
directory, so deleting it would leave them pointing at nothing, and dropping the
lock entry would remove the one record that lets `mdm skills list` and
`mdm doctor` report the breakage.

**If you meant to remove it everywhere**, drop the flag: `mdm skills remove demo`
sweeps every harness, the canonical directory, and the lock entry. Removing the
last remaining harness with `--harness` does the same thing, since at that point
nothing is left to keep it for.

Earlier versions deleted everything on a `--harness` removal, which is what left
other harnesses pointing at a missing directory with no lock entry to diagnose it
by. If a project still carries that damage, `mdm doctor` reports the broken links.

## `mdm skills add .` deleted the skills it was supposed to install

**Symptom.** On an older mdm, running `mdm skills add .` in a project that already
had skills installed emptied them and still printed a tick against each name.

**Cause.** Discovery walks the project for `SKILL.md` files and finds mdm's own
canonical copies under `.agents/skills`, so the install ran with the source
directory and the destination directory being one directory. Every install path
starts by emptying the destination, so the source was gone before any file was
read and the copy that followed had nothing left to copy.

**Fixed.** mdm now compares the source and the destination before removing
anything, and skips the copy when they are the same directory. The comparison
inspects the files rather than the path strings, so it also catches a harness's
`.claude/skills/<name>` symlink discovered as the source while the destination is
the `.agents/skills/<name>` it points at.

**Recovery on an affected project.** The emptied skills are not recoverable from
mdm - reinstall them from their sources with `mdm skills install`, or
`git restore` them if the canonical directory was committed.

## My copied skills came back as symlinks

**Symptom.** You installed with `--copy` expecting real directories, but
after `mdm skills install` (or `mdm skills update`) they come back as symlinks.

**Cause.** Versions before the install-mode switch did not record how a
project was installed, so restores and updates always defaulted to
symlinks, regardless of how the skill first arrived.

**Recovery.** Run `mdm migrate`. It inspects each configured harness's
install directory, or every harness the scope supports when the lock records
no harnesses, and records `installMode: copy` when it finds real skill
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

To go the other way, back from copies to symlinks, pass `--symlink` to the
next `mdm skills add` in that scope. It converts the copied installs into
links and records symlink mode, so the lock never needs editing by hand.

## A forked skill vanished after installing it to OpenClaw

Installing a skill replaces the harness's `<skills dir>/<name>`, and for OpenClaw
that path is inside the forks directory itself - so installing a fork there would
overwrite the fork with a symlink to a copy of itself.

`mdm skills cherry-pick --install` detects this and skips the harness, printing:

```
• OpenClaw already reads ./skills - skipping its install so the fork is not overwritten
```

That is not an error. The fork is already sitting where OpenClaw looks for
skills, so no install is needed. If you see the fork replaced by a symlink, you
are on a build from before this guard - upgrade with `mdm upgrade`.
