#!/usr/bin/env bash
set -e

REPO="sethcarney/mdm"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$OS" in
  linux)
    case "$ARCH" in
      x86_64)        TARGET="linux-x64" ;;
      aarch64|arm64) TARGET="linux-arm64" ;;
      *) echo "Unsupported architecture: $ARCH" && exit 1 ;;
    esac
    ;;
  darwin)
    case "$ARCH" in
      x86_64) TARGET="macos-x64" ;;
      arm64)  TARGET="macos-arm64" ;;
      *) echo "Unsupported architecture: $ARCH" && exit 1 ;;
    esac
    ;;
  *)
    echo "Unsupported OS: $OS"
    echo "For Windows, run the PowerShell installer:"
    echo "  irm https://raw.githubusercontent.com/${REPO}/main/install.ps1 | iex"
    echo ""
    echo "Or download mdm-windows-x64.exe directly from:"
    echo "  https://github.com/${REPO}/releases/latest"
    exit 1
    ;;
esac

BINARY_NAME="mdm-${TARGET}"
BASE_URL="https://github.com/${REPO}/releases/latest/download"

# GoReleaser signs the checksum manifest, not each binary, so the chain of
# trust runs: cosign verifies sha256sums.txt against its sigstore bundle, then
# the binary is verified against sha256sums.txt. Verifying a per-binary
# signature is not possible - no release has ever published one.
CHECKSUM_FILE="sha256sums.txt"
SIGNATURE_FILE="${CHECKSUM_FILE}.sigstore.json"

WORK_DIR="$(mktemp -d)"
cleanup() { rm -rf "$WORK_DIR"; }
trap cleanup EXIT

echo "Downloading mdm (${TARGET})..."
curl -fsSL "${BASE_URL}/${BINARY_NAME}" -o "${WORK_DIR}/${BINARY_NAME}"

echo "Downloading ${CHECKSUM_FILE}..."
curl -fsSL "${BASE_URL}/${CHECKSUM_FILE}" -o "${WORK_DIR}/${CHECKSUM_FILE}"

if command -v cosign >/dev/null 2>&1; then
  echo "Verifying the checksum manifest signature..."
  curl -fsSL "${BASE_URL}/${SIGNATURE_FILE}" -o "${WORK_DIR}/${SIGNATURE_FILE}"
  # The workflow is triggered by a tag push, so the signing identity always
  # ends in refs/tags/<tag>; it is never a branch ref.
  if ! cosign verify-blob "${WORK_DIR}/${CHECKSUM_FILE}" \
    --bundle "${WORK_DIR}/${SIGNATURE_FILE}" \
    --certificate-identity-regexp='^https://github\.com/sethcarney/mdm/\.github/workflows/release\.yml@refs/tags/.+$' \
    --certificate-oidc-issuer="https://token.actions.githubusercontent.com"; then
    echo "Signature verification FAILED for ${CHECKSUM_FILE}. Aborting." >&2
    exit 1
  fi
  echo "Signature verified."
else
  echo "cosign not found - skipping the signature check on ${CHECKSUM_FILE}."
  echo "Install cosign to verify it: https://docs.sigstore.dev/cosign/system_config/installation/"
fi

# Keep only this asset's line. A missing or duplicated entry means the manifest
# is not what we expect, and checking against it would prove nothing.
EXPECTED="$(grep -E "[[:space:]]\*?${BINARY_NAME}\$" "${WORK_DIR}/${CHECKSUM_FILE}" | awk '{print $1}')"
if [ "$(printf '%s\n' "$EXPECTED" | grep -c .)" -ne 1 ]; then
  echo "Expected exactly one checksum entry for ${BINARY_NAME} in ${CHECKSUM_FILE}. Aborting." >&2
  exit 1
fi

# sha256sum is coreutils; macOS ships shasum instead.
if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL="$(sha256sum "${WORK_DIR}/${BINARY_NAME}" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL="$(shasum -a 256 "${WORK_DIR}/${BINARY_NAME}" | awk '{print $1}')"
else
  # No hashing tool available. Warn loudly but proceed rather than block an
  # install on an exotic box without coreutils or perl. When cosign is present,
  # the step above has already verified the signed checksum manifest; this only
  # skips re-hashing the binary against it.
  ACTUAL=""
  echo "WARNING: neither sha256sum nor shasum found - cannot verify ${BINARY_NAME} against ${CHECKSUM_FILE}." >&2
  echo "WARNING: continuing without checksum verification. Install coreutils (sha256sum) or perl (shasum) to enable it." >&2
fi

if [ -n "$ACTUAL" ]; then
  echo "Verifying checksum..."
  if [ "$ACTUAL" != "$EXPECTED" ]; then
    echo "Checksum verification FAILED for ${BINARY_NAME}. The download may be corrupt or tampered. Aborting." >&2
    exit 1
  fi
  echo "Checksum verified."
fi

chmod +x "${WORK_DIR}/${BINARY_NAME}"

mkdir -p "$INSTALL_DIR"

echo "Installing to ${INSTALL_DIR}/mdm..."
if [ -w "$INSTALL_DIR" ]; then
  mv "${WORK_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/mdm"
else
  sudo mv "${WORK_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/mdm"
fi

echo ""
echo "mdm installed successfully!"

case ":$PATH:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    echo ""
    echo "Note: ${INSTALL_DIR} is not in your PATH."
    echo "Add the following to your ~/.bashrc, ~/.zshrc, or equivalent:"
    echo ""
    echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
    echo ""
    ;;
esac

echo "Verify with: mdm --version"
