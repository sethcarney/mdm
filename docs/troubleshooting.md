# Troubleshooting

Known sharp edges, what causes them, and how to get out of them.

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
