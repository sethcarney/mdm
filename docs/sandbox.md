# Sandbox — agent hardening

`mdm sandbox` configures the sandbox and permission settings of your AI coding
agents so they **cannot read secret files** and **stay confined to the project
working directory** — using each vendor's own native mechanism, from one
command.

Supported agents: **Claude Code**, **Codex**, and **GitHub Copilot CLI**.

```bash
mdm sandbox setup     # interactive: pick agents, review changes, apply
mdm sandbox status    # audit current config against the baseline
```

mdm only ever **adds or tightens** settings. It never removes entries you
wrote yourself, preserves unknown keys and comments in every file it touches,
and keeps stricter settings you already have (for example Codex's `read-only`
mode is not loosened to `workspace-write`).

## What gets configured

### Claude Code — `.claude/settings.json` (project-scoped, committable)

| Setting | Effect |
| --- | --- |
| `permissions.deny` `Read()` rules | Blocks reads of `.env*`, `*.pem`, `*.key`, `id_rsa`/`id_ed25519`, `secrets/`, `~/.ssh`, `~/.aws`, `~/.gnupg`, `~/.kube`, `~/.netrc`, `~/.npmrc`, `~/.config/gh`, and `~/.docker/config.json` |
| `permissions.disableBypassPermissionsMode: "disable"` | Prevents `--dangerously-skip-permissions` from being used at all |
| `sandbox.enabled: true` | Turns on OS-level enforcement (Seatbelt on macOS, bubblewrap on Linux/WSL2) so Bash commands and their child processes are confined to the working directory, with per-domain network approval |

Deny rules apply to Claude's file tools and recognized shell commands; the OS
sandbox extends enforcement to *all* Bash subprocesses, and merges the deny
rules into its boundary. On Linux/WSL2 install `bubblewrap` and `socat` for
the OS sandbox to activate.

### Codex — `$CODEX_HOME/config.toml` (global — Codex has no per-project config)

| Setting | Effect |
| --- | --- |
| `sandbox_mode = "workspace-write"` | Writes are confined to the workspace (existing `read-only` is kept) |
| `approval_policy = "on-request"` | Escaping the sandbox requires an approval prompt (existing `untrusted`/`on-failure` are kept; `never` is replaced) |
| `[sandbox_workspace_write]` `network_access = false` | Sandboxed commands cannot reach the network |

!!! warning "Codex cannot block file reads"
    Codex's sandbox confines **writes and network**, but both `read-only` and
    `workspace-write` modes allow reading any file on disk. `mdm sandbox
    status` reports this as an explicit *unsupported* check. Keep secrets out
    of reachable paths (or protect them with OS file permissions) when using
    Codex.

The TOML file is edited line-by-line, so comments and unrelated keys survive
untouched.

### GitHub Copilot CLI — hook + settings

Copilot CLI has no persistent deny rules (`--deny-tool` is per-session and
`permissions-config.json` only stores *approvals*), so mdm hardens it in two
halves:

| Where | What |
| --- | --- |
| `.github/hooks/mdm-sandbox.json` + `mdm-sandbox.sh` (project, committable) | A `preToolUse` hook that **denies** any file or shell tool call referencing a secret path. Command hooks are fail-closed: if the script crashes, the call is denied. |
| `~/.copilot/settings.json` (user) | `permissions.disableBypassPermissionsMode: "disable"` suppresses `--yolo`, `--allow-all`, and the other allow-all flags |

The guard script is POSIX `sh`, so it protects macOS/Linux sessions; on
native Windows it does not run. Template files (`.env.example`, `.env.sample`,
`.env.template`) are excluded from matching. If you hit a false positive, the
deny reason points at the script — adjust the pattern or delete the two hook
files to remove the guard.

Working-directory confinement is Copilot's default (paths outside trusted
folders prompt); `mdm sandbox status` warns when your trusted-folder list
contains `/` or your home directory.

## Scripting

```bash
mdm sandbox setup -y                       # non-interactive, all agents
mdm sandbox setup -a claude-code --dry-run # preview one agent's changes
mdm sandbox status --json                  # machine-readable audit (CI-friendly)
```

`status --json` emits one entry per agent with `checks[].state` of `ok`,
`missing`, `warn`, or `unsupported` — useful as a CI gate that your team's
committed agent config hasn't drifted below the baseline.

## Limitations

- These are *defense-in-depth* controls, not a substitute for OS-level user
  isolation: a determined prompt-injected agent has other channels, and each
  vendor's enforcement has documented gaps (see the Codex read limitation
  above, and Claude Code's sandbox docs on TLS/domain-fronting).
- Config changes take effect in **new** agent sessions — restart running ones.
- Env vars are respected when locating global config: `CODEX_HOME`,
  `COPILOT_HOME`.
