# mdm uninstall

Remove the mdm CLI binary from your system.

## Usage

```
mdm uninstall [flags]
```

Aliases: `remove-cli`

## Flags

| Flag    | Short | Default | Description                  |
| ------- | ----- | ------- | ---------------------------- |
| `--yes` | `-y`  | `false` | Skip the confirmation prompt |

## What it does

1. Resolves the path of the running `mdm` binary (following any symlinks).
2. Displays the binary location and asks for confirmation (unless `-y` is passed).
3. Deletes the binary.

On **Windows**, the binary cannot be deleted while it is running. `mdm uninstall` instead writes a small batch script to the system temp directory and launches it in the background. The binary is removed after the current process exits.

Skills, lock files, and agent configuration are **not** affected — only the `mdm` binary itself is removed.

## Examples

```bash
# Interactive — shows the binary path and asks for confirmation
mdm uninstall

# Non-interactive — skip the confirmation prompt
mdm uninstall -y

# Using the alias
mdm remove-cli
```

## Re-installing

To reinstall mdm after uninstalling, use the install script:

```bash
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/sethcarney/mdm/main/install.sh | bash

# Windows (PowerShell)
iwr https://raw.githubusercontent.com/sethcarney/mdm/main/install.ps1 | iex
```

Or download a binary directly from the [releases page](https://github.com/sethcarney/mdm/releases/latest).
