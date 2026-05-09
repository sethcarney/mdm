---
name: go-report-card
category: go
description: Run Go quality checks on the current module — tests, vulnerability scan, cyclomatic complexity, and formatting. Invoke before opening a PR or when the user wants a quick quality snapshot.
user-invocable: true
allowed-tools: Bash
---

# Go Report Card

Run the standard quality checks for this module. Stop at the first failure and surface the output clearly.

## Checks

Run in order:

```bash
# 1. Format (auto-fixes in place)
gofmt -s -w .

# 2. Tests
go test ./...

# 3. Vulnerability scan (install if missing)
go install golang.org/x/vuln/cmd/govulncheck@v1.1.4 && govulncheck ./...

# 4. Cyclomatic complexity (flag anything over 16)
go install github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 && gocyclo -over 16 .
```

## Report

After all checks, output a one-line status per check:

```
gofmt       ✓ pass
go test     ✗ fail   FAIL: github.com/user/pkg (0.12s)
govulncheck ✓ pass
gocyclo     ✓ pass
```

If anything failed, list the specific errors underneath and stop. Don't move on until the user has addressed them.

