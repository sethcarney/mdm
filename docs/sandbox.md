# Sandbox — agent hardening

`mdm sandbox` configures the sandbox and permission settings of your AI coding
agents so they **cannot read secret files** and **stay confined to the project
working directory** — using each vendor's own native mechanism, from one
command.

Supported agents: **Claude Code**, **Codex**, **GitHub Copilot CLI**,
**Cursor**, and **Gemini CLI**.

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
| `permissions.deny` `Read()` rules | Blocks reads of `.env*`, `*.pem`, `*.key`, `id_rsa`/`id_ed25519`, `secrets/`, `*credentials*`, `appsettings.*.json`, `.npmrc`, `nuget.config`, plus home credential stores (`~/.ssh`, `~/.aws`, `~/.gnupg`, `~/.kube`, `~/.netrc`, `~/.npmrc`, `~/.config/gh`, `~/.docker/config.json`, `~/.microsoft/usersecrets`, `~/.nuget`) |
| `permissions.disableBypassPermissionsMode: "disable"` | Prevents `--dangerously-skip-permissions` from being used at all |
| `sandbox.enabled: true` | Turns on OS-level enforcement (Seatbelt on macOS, bubblewrap on Linux/WSL2) so Bash commands and their child processes are confined to the working directory, with per-domain network approval |
| `sandbox.allowUnsandboxedCommands: false` | Strict mode — the escape hatch that retries a failed command *outside* the sandbox is disabled |
| `sandbox.filesystem.denyRead: ["~/", "./**/…"]` + `allowRead: ["."]` | The part that actually blocks secret **reads** at the OS layer: denies the whole home directory (re-allowing the project via `allowRead`) plus in-project secret globs |

!!! note "Why the `filesystem` block matters"
    `sandbox.enabled: true` alone confines *writes* and gates network, but the
    default read policy still lets sandboxed commands read the entire disk —
    including `~/.aws/credentials` and `~/.ssh`. The `sandbox.filesystem`
    `denyRead`/`allowRead` pair is what turns on OS-level secret-**read**
    blocking, so mdm always writes it alongside `enabled`.

Deny rules apply to Claude's file tools and recognized shell commands; the OS
sandbox extends enforcement to *all* Bash subprocesses. On Linux/WSL2 install
`bubblewrap` and `socat` for the OS sandbox to activate. If a build legitimately
needs to read from `$HOME`, widen the boundary with `sandbox.filesystem.allowRead`.

Equivalent deny rules are de-duplicated: `Read(.env)` and `Read(**/.env)` mean
the same thing under Claude's gitignore semantics, so mdm never writes both —
your existing rules are detected and left as-is.

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

### Cursor — `.cursor/cli.json` (project-scoped, committable)

Cursor's CLI agent uses the same `permissions.allow`/`permissions.deny` token
shape as Claude Code (`Read()`, `Shell()`, `Write()`), where deny always beats
allow. mdm writes `Read()` deny rules for the secret paths above into
`.cursor/cli.json`.

Cursor also reads a global `~/.cursor/cli-config.json`; the project file is the
committable, team-shared layer. Cursor's per-command OS sandbox (filesystem and
network isolation) is a separate mechanism enabled through the agent's run
mode — these rules harden the file-configurable permission layer.

### Gemini CLI — `.gemini/settings.json` + user settings

| Where | What |
| --- | --- |
| `~/.gemini/settings.json` (user) | `security.folderTrust.enabled: true` — untrusted workspaces run in "safe mode" (project settings, `.env` files, and MCP servers are not loaded) |
| `.gemini/settings.json` (project, committable) | `tools.sandbox: true` — every shell tool call is isolated, limiting filesystem access to the project directory |

!!! warning "Gemini cannot block individual file reads"
    Like Codex, Gemini has no per-file read-deny. It confines reads through
    sandbox **workspace isolation**, so secrets *outside* the workspace become
    unreadable while in-workspace files do not. `mdm sandbox status` reports
    secret-read blocking as *unsupported*. Also note the sandbox needs a
    container runtime (Docker/Podman) on Linux; macOS can use the built-in
    `sandbox-exec`. An existing `tools.sandbox` mechanism (e.g. `"podman"`) is
    kept as-is.

## Doctor integration

`mdm doctor` includes an **Agent sandboxing** section that runs these same
baseline checks for every supported agent that is configured in the project or
detected on your machine, surfacing any below-baseline items as warnings.
Run `mdm doctor` for a quick health read, or `mdm sandbox status` for the full
per-agent breakdown.

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
  `COPILOT_HOME`, `GEMINI_HOME`, `CLAUDE_CONFIG_DIR`.
