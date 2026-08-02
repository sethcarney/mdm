#!/usr/bin/env bash
#
# Dev Container Feature installer for mdm.
#
# Runs as root during image build, before the workspace is mounted, so it does
# nothing but put a verified binary on the PATH. Anything that touches the repo
# (`mdm skills install`, `mdm rules link`) belongs in a postCreateCommand, which
# runs after the workspace exists.
#
# The binary comes from the GitHub release matching the `version` option, and is
# checked against that release's sha256sums.txt before it is installed.
#
# tests/devcontainer_feature_test.go fails the build if the asset names, the
# checksum file name, or the repository here drift from .goreleaser.yaml.

set -euo pipefail

# Option values arrive as environment variables named after the option, upper
# cased. Defaults repeated here so the script also works when run by hand.
VERSION="${VERSION:-latest}"

REPO="sethcarney/mdm"

# /usr/local/bin rather than ~/.local/bin: it is on PATH for every user in a
# container, so the feature works regardless of which remoteUser the image ends
# up running as, and root-owned it stays out of reach of the dev user.
INSTALL_DIR="/usr/local/bin"
BINARY_NAME="mdm"

# Name of the checksum manifest GoReleaser attaches to each release.
CHECKSUM_FILE="sha256sums.txt"

if [ "$(id -u)" -ne 0 ]; then
	echo "mdm-feature: must run as root (dev container features install during image build)" >&2
	exit 1
fi

# --- dependencies -----------------------------------------------------------

# Debian/Ubuntu base images vary in how stripped down they are; curl and the CA
# bundle in particular are missing from the slim variants. Install only what is
# actually absent, and only once — apt-get update on every feature build is slow.
apt_updated=false
ensure_packages() {
	local missing=()
	local pkg
	for pkg in "$@"; do
		dpkg -s "$pkg" >/dev/null 2>&1 || missing+=("$pkg")
	done
	[ ${#missing[@]} -eq 0 ] && return 0

	if ! command -v apt-get >/dev/null 2>&1; then
		echo "mdm-feature: missing ${missing[*]} and no apt-get to install them." >&2
		echo "mdm-feature: this feature supports Debian and Ubuntu based images." >&2
		exit 1
	fi

	if [ "$apt_updated" = false ]; then
		echo "==> apt-get update"
		apt-get update -y
		apt_updated=true
	fi
	echo "==> installing ${missing[*]}"
	apt-get install -y --no-install-recommends "${missing[@]}"
}

# coreutils supplies sha256sum, which the verification step below depends on.
ensure_packages curl ca-certificates coreutils

# --- architecture -----------------------------------------------------------

# On an Apple Silicon host the container is normally arm64, so `uname -m`
# reports aarch64 and the arm64 release binary is the right one. An amd64
# container under emulation reports x86_64 and gets the x64 binary; both work.
case "$(uname -m)" in
x86_64 | amd64)
	ARCH="x64"
	;;
aarch64 | arm64)
	ARCH="arm64"
	;;
*)
	echo "mdm-feature: unsupported architecture '$(uname -m)'." >&2
	echo "mdm-feature: releases are published for linux x86_64 and aarch64 only." >&2
	exit 1
	;;
esac

ASSET="mdm-linux-${ARCH}"

# --- resolve the release tag ------------------------------------------------

# 'latest' is resolved through the /releases/latest redirect rather than the
# REST API: the redirect is not rate limited, so an unauthenticated image build
# on a busy runner cannot fail with HTTP 403. GitHub redirects to
# .../releases/tag/<tag>, and the tag is the last path segment.
resolve_latest_tag() {
	local url
	url="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest")"
	case "$url" in
	*/releases/tag/*)
		printf '%s' "${url##*/releases/tag/}"
		;;
	*)
		echo "mdm-feature: could not resolve the latest release (ended up at '${url}')." >&2
		exit 1
		;;
	esac
}

if [ "$VERSION" = "latest" ] || [ -z "$VERSION" ]; then
	TAG="$(resolve_latest_tag)"
	echo "==> latest release resolves to ${TAG}"
else
	# Accept both '1.9.1' and 'v1.9.1'; release tags always carry the v.
	TAG="v${VERSION#v}"
fi

BASE_URL="https://github.com/${REPO}/releases/download/${TAG}"

# --- download and verify ----------------------------------------------------

WORK_DIR="$(mktemp -d)"
cleanup() {
	rm -rf "$WORK_DIR"
	# Only if this script is what populated them: feature installs run in a
	# build layer, where the apt lists are dead weight in the final image.
	if [ "$apt_updated" = true ]; then
		rm -rf /var/lib/apt/lists/*
	fi
}
trap cleanup EXIT

echo "==> downloading ${ASSET} (${TAG})"
if ! curl -fsSL "${BASE_URL}/${ASSET}" -o "${WORK_DIR}/${ASSET}"; then
	echo "mdm-feature: could not download ${BASE_URL}/${ASSET}" >&2
	echo "mdm-feature: check that '${TAG}' is a published release of ${REPO}." >&2
	exit 1
fi

echo "==> downloading ${CHECKSUM_FILE}"
curl -fsSL "${BASE_URL}/${CHECKSUM_FILE}" -o "${WORK_DIR}/${CHECKSUM_FILE}"

# Keep only this asset's line. A missing or duplicated entry means the manifest
# is not what we expect, and verifying against it would prove nothing — an empty
# checksum file makes `sha256sum -c` succeed by checking zero files.
grep -E "[[:space:]]\*?${ASSET}\$" "${WORK_DIR}/${CHECKSUM_FILE}" >"${WORK_DIR}/expected.sha256" || true
matches="$(wc -l <"${WORK_DIR}/expected.sha256")"
if [ "$matches" -ne 1 ]; then
	echo "mdm-feature: expected exactly one checksum entry for ${ASSET}, found ${matches}." >&2
	exit 1
fi

echo "==> verifying checksum"
if ! (cd "$WORK_DIR" && sha256sum -c expected.sha256); then
	echo "mdm-feature: checksum verification FAILED for ${ASSET} — refusing to install." >&2
	exit 1
fi

# --- install ----------------------------------------------------------------

install -m 0755 "${WORK_DIR}/${ASSET}" "${INSTALL_DIR}/${BINARY_NAME}"

echo "==> installed ${TAG} to ${INSTALL_DIR}/${BINARY_NAME}"

# Also a smoke test: a binary built for the wrong architecture, or truncated
# despite matching its checksum, fails here instead of at first use.
"${INSTALL_DIR}/${BINARY_NAME}" --version
