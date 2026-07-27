#!/usr/bin/env bash
#
# Dev container postCreate hook.
#
# Installs the same tool versions CI uses so `make ci`, the Pre-PR checklist in
# AGENTS.md, and a local release dry-run all behave the same inside the
# container as they do on a runner. Tools are built with `go install` under the
# pinned GOTOOLCHAIN rather than pulled as prebuilt binaries: golangci-lint has
# to be compiled with the project's Go to parse its go1.26 sources.
#
# Safe to re-run — already-installed tools at the pinned version are skipped.

set -euo pipefail

# Source of truth for the tool versions. Keep in step with
# .github/workflows/ci.yml and release.yml; tests/devcontainer_test.go fails
# the build if they drift.
GOLANGCI_LINT_VERSION="v2.12.2"
GOVULNCHECK_VERSION="v1.5.0"
GORELEASER_VERSION="v2.15.4"

cd "$(dirname "${BASH_SOURCE[0]}")/.."

# The Go version has exactly one source of truth: the `go` directive in go.mod.
#
# The pin has to be explicit, because under the default GOTOOLCHAIN=auto the
# go.mod version is only a *minimum* — a newer toolchain in the base image would
# silently win, and the container would stop matching CI. Writing it to the
# persistent Go env file (rather than exporting it, or setting containerEnv in
# devcontainer.json) means every `go` command honours it in any shell, and that
# the version stays defined in one place only.
GO_VERSION="$(awk '$1 == "go" { print $2; exit }' go.mod)"
if [[ ! "$GO_VERSION" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?$ ]]; then
	echo "post-create: could not read the go directive from go.mod (got '${GO_VERSION}')" >&2
	exit 1
fi
go env -w "GOTOOLCHAIN=go${GO_VERSION}"
echo "==> pinned GOTOOLCHAIN=go${GO_VERSION} (from go.mod)"

# Claude Code's credentials live in the named volume mounted at ~/.claude (see
# devcontainer.json). Docker creates a fresh volume owned by root when its mount
# point does not already exist in the image, which would leave Claude Code
# unable to write its token. Fix that once, only when it actually needs fixing —
# a recursive chown over an established session history is not free.
CLAUDE_DIR="${HOME}/.claude"
if [ -d "$CLAUDE_DIR" ] && [ ! -w "$CLAUDE_DIR" ]; then
	echo "==> taking ownership of ${CLAUDE_DIR}"
	sudo chown -R "$(id -u):$(id -g)" "$CLAUDE_DIR"
fi

# installed_version prints the module version a Go binary on PATH was built
# from, or nothing if the binary is missing or wasn't built by Go.
installed_version() {
	local bin_path
	bin_path="$(command -v "$1" 2>/dev/null)" || return 0
	go version -m "$bin_path" 2>/dev/null | awk '$1 == "mod" { print $3; exit }'
}

install_pinned() {
	local bin="$1" pkg="$2" version="$3"

	if [ "$(installed_version "$bin")" = "$version" ]; then
		echo "==> $bin $version already installed"
		return
	fi

	echo "==> installing $bin $version"
	go install "$pkg@$version"
}

echo "==> go version: $(go version)"
echo "==> git version: $(git --version)"

echo "==> downloading module dependencies"
go mod download

install_pinned golangci-lint github.com/golangci/golangci-lint/v2/cmd/golangci-lint "$GOLANGCI_LINT_VERSION"
install_pinned govulncheck golang.org/x/vuln/cmd/govulncheck "$GOVULNCHECK_VERSION"
install_pinned goreleaser github.com/goreleaser/goreleaser/v2 "$GORELEASER_VERSION"

echo "==> dev container ready. Try: make build && make test"
