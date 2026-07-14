# Installation

`mdm` is a single, statically-linked Go binary with no runtime dependencies.

## One-line install

=== "macOS / Linux"

    ```bash
    curl -fsSL https://raw.githubusercontent.com/sethcarney/mdm/main/install.sh | bash
    ```

=== "Windows (PowerShell)"

    ```powershell
    irm https://raw.githubusercontent.com/sethcarney/mdm/main/install.ps1 | iex
    ```

Both installers place the binary at `~/.local/bin/mdm` and warn if that
directory isn't on your `PATH`.

### Install to a custom directory

Set `INSTALL_DIR` before running the installer:

```bash
INSTALL_DIR=/usr/local/bin curl -fsSL https://raw.githubusercontent.com/sethcarney/mdm/main/install.sh | bash
```

## Other methods

=== "Go install"

    ```bash
    go install github.com/sethcarney/mdm@latest
    ```

    This installs to `$GOPATH/bin`. The version reported by `mdm --version`
    will be `dev` for `go install` builds — released binaries carry the real
    tag.

=== "Download a release"

    Grab a prebuilt binary for Linux, macOS, or Windows (x64 + ARM64) from the
    [GitHub Releases page](https://github.com/sethcarney/mdm/releases/latest),
    then move it onto your `PATH`.

=== "Build from source"

    ```bash
    git clone https://github.com/sethcarney/mdm
    cd mdm
    make build      # compiles to ./mdm
    make install    # go install . → $GOPATH/bin
    ```

## Verify the install

```bash
mdm --version
mdm --help
```

## Shell completion

`mdm` ships completion scripts for bash, zsh, fish, and PowerShell:

```bash
# Print a script to stdout
mdm completion zsh

# Or write it into your shell rc automatically
mdm completion install
```

## Keeping mdm up to date

```bash
mdm upgrade            # download and replace the binary with the latest release
mdm upgrade --beta     # opt into the latest prerelease
```

See the [upgrade guide](upgrade.md) for details, and
[uninstall](uninstall.md) to remove the binary.

## Next steps

<div class="grid cards" markdown>

-   :material-source-branch: __[Link your instruction files](rules.md)__

    Make `AGENTS.md` the single source of truth across every agent.

-   :material-download-box: __[Add your first skill](skills/add.md)__

    Install from GitHub, GitLab, a URL, or the skills.sh registry.

-   :material-book-open-variant: __[Browse the full command reference](commands.md)__

    Every command, flag, and alias in one place.

</div>
