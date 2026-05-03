# Security Policy

## Reporting a Vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.

Report vulnerabilities by emailing **seth@eqengineered.com**. Include a description of the issue, steps to reproduce, and any relevant versions.

You should receive a response within 72 hours. If the issue is confirmed, a fix will be prioritized and a patched release issued as soon as possible.

## Supported Versions

Only the latest release receives security fixes.

## Release Verification

All release binaries are accompanied by a `sha256sums.txt` file and a Sigstore cosign bundle (`.bundle`). You can verify a download using either method.

### Verify with cosign (recommended)

Each binary is signed with [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) keyless signing via Sigstore, tied to the official GitHub Actions OIDC identity. No GPG keys or secrets are required.

```bash
cosign verify-blob mdm-linux-x64 \
  --bundle mdm-linux-x64.bundle \
  --certificate-identity-regexp='^https://github\.com/sethcarney/mdm/\.github/workflows/release\.yml@refs/heads/main$' \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com"
```

Replace `mdm-linux-x64` and `mdm-linux-x64.bundle` with the appropriate filenames for your platform:

| Platform        | Binary                  | Bundle                        |
|-----------------|-------------------------|-------------------------------|
| Linux x64       | `mdm-linux-x64`         | `mdm-linux-x64.bundle`        |
| Linux ARM64     | `mdm-linux-arm64`       | `mdm-linux-arm64.bundle`      |
| macOS x64       | `mdm-macos-x64`         | `mdm-macos-x64.bundle`        |
| macOS ARM64     | `mdm-macos-arm64`       | `mdm-macos-arm64.bundle`      |
| Windows x64     | `mdm-windows-x64.exe`   | `mdm-windows-x64.exe.bundle`  |

Both the binary and its `.bundle` file are attached to each [GitHub release](https://github.com/sethcarney/mdm/releases).

### Verify with SHA-256

```bash
sha256sum -c sha256sums.txt --ignore-missing
```

The `sha256sums.txt` file is attached to each [GitHub release](https://github.com/sethcarney/mdm/releases).
