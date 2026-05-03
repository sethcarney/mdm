# Security Policy

## Reporting a Vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.

Report vulnerabilities by emailing **seth@eqengineered.com**. Include a description of the issue, steps to reproduce, and any relevant versions.

You should receive a response within 72 hours. If the issue is confirmed, a fix will be prioritized and a patched release issued as soon as possible.

## Supported Versions

Only the latest release receives security fixes.

## Release Verification

All release binaries are signed with [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) keyless signing via Sigstore and accompanied by a `sha256sums.txt` checksum file. Each release includes `.sig`, `.pem`, and `.bundle` files for every binary.

### Verify with cosign

The signature is tied to the official GitHub Actions OIDC identity — no GPG keys or secrets required.

**Using the `.sig` + `.pem` files:**
```bash
cosign verify-blob mdm-linux-x64 \
  --signature mdm-linux-x64.sig \
  --certificate mdm-linux-x64.pem \
  --certificate-identity-regexp='^https://github\.com/sethcarney/mdm/\.github/workflows/release\.yml@refs/heads/main$' \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com"
```

**Using the `.bundle` file:**
```bash
cosign verify-blob mdm-linux-x64 \
  --bundle mdm-linux-x64.bundle \
  --certificate-identity-regexp='^https://github\.com/sethcarney/mdm/\.github/workflows/release\.yml@refs/heads/main$' \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com"
```

Replace `mdm-linux-x64` with the appropriate filename for your platform:

| Platform    | Binary                | Signature                   | Certificate                 |
|-------------|-----------------------|-----------------------------|-----------------------------|
| Linux x64   | `mdm-linux-x64`       | `mdm-linux-x64.sig`         | `mdm-linux-x64.pem`         |
| Linux ARM64 | `mdm-linux-arm64`     | `mdm-linux-arm64.sig`       | `mdm-linux-arm64.pem`       |
| macOS x64   | `mdm-macos-x64`       | `mdm-macos-x64.sig`         | `mdm-macos-x64.pem`         |
| macOS ARM64 | `mdm-macos-arm64`     | `mdm-macos-arm64.sig`       | `mdm-macos-arm64.pem`       |
| Windows x64 | `mdm-windows-x64.exe` | `mdm-windows-x64.exe.sig`   | `mdm-windows-x64.exe.pem`   |

All verification files are attached to each [GitHub release](https://github.com/sethcarney/mdm/releases).

### Verify with SHA-256

```bash
sha256sum -c sha256sums.txt --ignore-missing
```

The `sha256sums.txt` file is attached to each [GitHub release](https://github.com/sethcarney/mdm/releases).
