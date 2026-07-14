---
hide:
  - navigation
  - toc
---

<div class="mdm-hero" markdown>

<img class="mdm-hero__logo" src="assets/logo-white.svg" alt="mdm logo">

# mdm

<p class="mdm-hero__tagline">
The markdown management CLI. Install skills, keep every agent's instruction file
in sync, and audit them all for prompt-injection risks — from one fast, Go-native tool.
<strong>No telemetry. Fully open source.</strong>
</p>

<div class="mdm-cta" markdown>
[Get started :material-arrow-right:](installation.md){ .md-button .md-button--primary }
[Command reference](commands.md){ .md-button }
[View on GitHub](https://github.com/sethcarney/mdm){ .md-button }
</div>

</div>

---

## Install in one line

=== "macOS / Linux"

    ```bash
    curl -fsSL https://raw.githubusercontent.com/sethcarney/mdm/main/install.sh | bash
    ```

=== "Windows (PowerShell)"

    ```powershell
    irm https://raw.githubusercontent.com/sethcarney/mdm/main/install.ps1 | iex
    ```

Then link your instruction files and add your first skill:

```bash
mdm rules link                          # AGENTS.md becomes the source of truth
mdm skills add anthropics/skills        # install a skill for every configured agent
```

See the [installation guide](installation.md) for other methods and PATH tips.

---

## Why mdm?

Keeping `CLAUDE.md`, `copilot-instructions.md`, `.cursorrules`, and friends
consistent is tedious. Add in installing, versioning, updating, and auditing
skills across a dozen tools and it becomes a real maintenance burden. `mdm`
solves exactly that.

<div class="grid cards" markdown>

-   :material-robot-happy:{ .lg .middle } __45 agents supported__

    ---

    Claude Code, Cursor, Cline, GitHub Copilot, Gemini CLI, Codex, and 39 more —
    out of the box.

-   :material-source-branch:{ .lg .middle } __One source of truth__

    ---

    `mdm rules link` makes `AGENTS.md` canonical and symlinks every agent's
    expected filename to it.

    [:octicons-arrow-right-24: Rules guide](rules.md)

-   :material-download-box:{ .lg .middle } __Skills from anywhere__

    ---

    Install from GitHub, GitLab, arbitrary URLs, local paths, or the
    [skills.sh](https://skills.sh) registry.

    [:octicons-arrow-right-24: skills add](skills/add.md)

-   :material-lock-check:{ .lg .middle } __Security-focused by default__

    ---

    Every install runs a deterministic scan for hidden characters and
    prompt-smuggling patterns. `mdm skills audit` checks OSV advisories.

    [:octicons-arrow-right-24: Security scans](security/hidden-character-scan.md)

-   :material-sync:{ .lg .middle } __Reproducible installs__

    ---

    Commit a `skills-lock.json` so teammates run `mdm skills install` once and
    onboard with whatever agent they prefer.

    [:octicons-arrow-right-24: skills install](skills/install.md)

-   :material-flask-outline:{ .lg .middle } __Knowledge bundles__ _(experimental)_

    ---

    `mdm knowledge` installs, validates, and updates Open Knowledge Format (OKF)
    bundles behind an experimental flag.

    [:octicons-arrow-right-24: Experimental features](experimental.md)

</div>

---

## The command surface at a glance

```text
mdm
├── skills        Manage skills for AI agents (add · remove · list · find · update · audit · init · install · sync)
├── rules         Link/unlink agent instruction files to a single AGENTS.md
├── agents        Manage the configured agent list used as default install targets
├── knowledge     [experimental] Manage OKF knowledge bundles
├── doctor        Check installed skills and project markdown for health issues
├── experimental  Manage experimental feature gates
├── upgrade       Self-update the mdm binary from GitHub releases
├── uninstall     Remove the mdm binary from your system
└── completion    Generate shell completion scripts
```

Every command, flag, and alias is documented on the
[**Command reference**](commands.md) page.

---

!!! tip "Prefer a UI?"
    There's also an [mdm VS Code extension](https://marketplace.visualstudio.com/items?itemName=SethsSoftware.mdm-sidebar)
    that wraps the same functionality in a sidebar.
