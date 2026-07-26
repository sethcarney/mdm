#!/usr/bin/env bash
#
# Dev container postCreate hook.
#
# Installs the same tool versions CI uses so `make ci`, the Pre-PR checklist in
# AGENTS.md, and a local release dry-run all behave the same inside the
# container as they do on a runner. Tools are built with `go install` under the
# pinned GOTOOLCHAIN (see .devcontainer/devcontainer.json) rather than pulled as
# prebuilt binaries: golangci-lint has to be compiled with the project's Go to
# parse its go1.26 sources.
#
# Safe to re-run — already-installed tools at the pinned version are skipped.

set -euo pipefail

# Keep these in sync with .github/workflows/ci.yml and release.yml.
GOLANGCI_LINT_VERSION="v2.12.2"
GOVULNCHECK_VERSION="v1.5.0"
GORELEASER_VERSION="v2.15.4"

cd "$(dirname "${BASH_SOURCE[0]}")/.."

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
