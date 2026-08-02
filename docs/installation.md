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

## Dev container

mdm is published as a
[Dev Container Feature](https://containers.dev/implementors/features/), so a
repository can declare the CLI in `devcontainer.json` instead of scripting an
install:

```jsonc
"features": {
  "ghcr.io/sethcarney/mdm/mdm:1": {}
}
```

The feature detects the container's architecture, downloads the matching release
binary, verifies it against that release's `sha256sums.txt`, and installs it to
`/usr/local/bin/mdm` — on `PATH` for every user, and no Go toolchain required in
the image.

Pin a release when the container needs to be reproducible:

```jsonc
"features": {
  "ghcr.io/sethcarney/mdm/mdm:1": {
    "version": "1.9.1"
  }
}
```

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `version` | string | `latest` | Release of mdm to install. `latest`, or a release tag such as `1.9.1` (the leading `v` is optional). |

Features install during **image build**, before the workspace is mounted, so the
feature only puts the binary on `PATH`. Anything that reads or writes the repo
goes in a lifecycle command:

```jsonc
"features": {
  "ghcr.io/sethcarney/mdm/mdm:1": {}
},
"postCreateCommand": "mdm skills install"
```

That restores every skill recorded in the repo's `skills-lock.json` when the
container is created — see [`mdm skills install`](skills/install.md). `mdm rules
link` fits the same slot.

!!! note "Supported platforms"

    Debian- and Ubuntu-based images on `linux/amd64` and `linux/arm64`. On an
    Apple Silicon host the container is normally arm64 and gets the arm64
    binary; an amd64 container under emulation gets the x64 one.

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
