# Security Policy

## Reporting a Vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.

Report vulnerabilities by emailing **seth@eqengineered.com**. Include a description of the issue, steps to reproduce, and any relevant versions.

You should receive a response within 72 hours. If the issue is confirmed, a fix will be prioritized and a patched release issued as soon as possible.

## Supported Versions

Only the latest release receives security fixes.

## Release Verification

All release binaries are accompanied by a `sha256sums.txt` file. Verify a download:

```bash
sha256sum -c sha256sums.txt --ignore-missing
```

The `sha256sums.txt` file is attached to each [GitHub release](https://github.com/sethcarney/mdm/releases).
