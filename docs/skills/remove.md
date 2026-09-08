# mdm skills remove

Remove installed skills.

## Usage

```
mdm skills remove [skills...]
```

Without arguments, an interactive multiselect lets you choose which skills to remove. With skill names provided, those skills are removed directly (with a confirmation prompt unless `--yes` is passed).

## Flags

| Flag           | Description                                   |
| -------------- | --------------------------------------------- |
| `--global, -g` | Remove from global scope                        |
| `--harness`    | Remove from specific harnesses only (repeatable) |
| `--skill, -s`  | Skill names to remove (repeatable)              |
| `--yes, -y`    | Skip confirmation prompts                       |
| `--all`        | Remove every skill without prompting (shorthand for `--skill '*' -y`)    |

## Examples

```bash
# Interactive - pick scope, then pick skills to remove
mdm skills remove

# Remove a specific skill (prompts for scope)
mdm skills remove my-skill

# Remove multiple skills without prompting
mdm skills remove skill1 skill2 -y

# Remove a globally installed skill
mdm skills remove --global my-skill

# Remove all installed skills
mdm skills remove --all
```

## What gets removed

Without `--harness`, the skill is being removed outright, so mdm removes:

- Every harness's symlink or copy (e.g. `.claude/skills/<skill>`, `.roo/skills/<skill>`).
  The sweep covers **every** harness mdm knows about, not only the ones the skill
  was detected in. Detection depends on the harness's tool being installed on this
  machine, so a directory left behind by an earlier `--harness X` install would
  otherwise keep a link to a canonical directory that no longer exists.
- The canonical skill directory (`.agents/skills/<skill>` for project,
  `~/.agents/skills/<skill>` for global).
- The entry in `mdm.lock`.

## Removing from one harness

`--harness` is genuinely scoped. mdm removes that harness's copy, then decides
from the harnesses **outside** the filter whether anything still needs the
canonical directory:

- **Something else still has the skill.** The canonical directory and the lock
  entry are kept, and mdm says so:

  ```
  ! demo: removed from Claude Code, but Roo Code still has it - keeping the skill and its lock entry
  ```

  Keeping both matters because every other harness installs by symlinking the
  canonical directory. Deleting it would leave them pointing at nothing, and
  dropping the lock entry would take away the one record that lets
  `mdm skills list` and `mdm doctor` notice.

- **Nothing else has it.** The named harness was the last one, so the removal
  finishes the same way an unfiltered one does - canonical directory and lock
  entry included - and reports `Removed <skill>`.

A harness that reads the shared `.agents/skills` directory is skipped while
something outside the filter still holds the skill: its "own" copy *is* the copy
those harnesses are reading, so there is nothing scoped to delete for it.

Which harnesses count as still holding the skill depends on the kind:

- A harness with a skills directory of its own counts when that directory holds
  the skill. Nothing but an install puts it there.
- A harness that reads the shared `.agents/skills` directory proves nothing by
  its contents, so it counts only when this project configured it or its tool is
  installed on this machine.

## When a deletion fails

If a file could not be deleted, mdm reports the failure against the skill instead
of printing `Removed`, and the lock entry is deliberately left in place. The lock
is the record of what is on disk: dropping the entry for a skill whose files are
still there would hide the failure from every later command.

## Orphan cleanup

When `mdm skills remove` finds no installed skills to remove, it automatically scans the lock file for entries whose directories no longer exist on disk and cleans them up. This handles cases where skill files were deleted manually.
