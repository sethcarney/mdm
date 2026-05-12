# mdm upgrade

Upgrade the mdm CLI binary to the latest release.

## Usage

```
mdm upgrade
```

Checks the latest release on GitHub, downloads the binary for your platform, verifies its SHA256 checksum, and replaces the running binary in place. No separate install step required.

Aliases: `update-cli`, `self-update`

## Flags

| Flag       | Description                                                                  |
| ---------- | ---------------------------------------------------------------------------- |
| `--stable` | Upgrade to the latest stable release (default behavior; mutually exclusive with `--beta`) |
| `--beta`   | Upgrade to the latest beta / prerelease tag instead of the stable release    |

`mdm upgrade` skips prereleases by default because GitHub's `/releases/latest` API excludes them. Pass `--beta` to opt in to release candidates and other prerelease tags.

## What it does

1. Queries the GitHub releases API for the latest version.
2. Compares it against the currently running version. If already up to date, exits.
3. Downloads the platform-specific binary and the `sha256sums.txt` checksum file.
4. Verifies the SHA256 digest before writing anything to disk.
5. Replaces the current executable atomically (via rename on Unix, a background batch script on Windows).

## Platform support

| OS | Architectures |
|---|---|
| Linux | x64, arm64 |
| macOS | x64, arm64 (Apple Silicon) |
| Windows | x64 |

## Examples

```bash
# Upgrade to the latest release
mdm upgrade

# Using an alias
mdm self-update
mdm update-cli
```

## Already up to date

If you are on the latest version:

```
Already up to date (1.2.3)
```

## After upgrading

On Unix systems the binary is replaced immediately. Restart your shell or run `mdm --version` to confirm the new version is active.

On Windows, the replacement happens in a background process after the current command exits. PowerShell or Command Prompt may need to be restarted.

## Manual download

If `mdm upgrade` cannot reach GitHub (air-gapped environment, corporate proxy, etc.), download the binary for your platform directly from the [releases page](https://github.com/sethcarney/mdm/releases/latest) and replace the binary manually.
