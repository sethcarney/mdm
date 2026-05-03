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
DOWNLOAD_URL="https://github.com/${REPO}/releases/latest/download/${BINARY_NAME}"

echo "Downloading mdm (${TARGET})..."
curl -fsSL "$DOWNLOAD_URL" -o /tmp/mdm-install

if command -v cosign >/dev/null 2>&1; then
  echo "Verifying cosign signature..."
  BUNDLE_URL="https://github.com/${REPO}/releases/latest/download/${BINARY_NAME}.bundle"
  curl -fsSL "$BUNDLE_URL" -o /tmp/mdm-install.bundle
  cosign verify-blob /tmp/mdm-install \
    --bundle /tmp/mdm-install.bundle \
    --certificate-identity-regexp='^https://github\.com/sethcarney/mdm/\.github/workflows/release\.yml@refs/heads/main$' \
    --certificate-oidc-issuer="https://token.actions.githubusercontent.com" || {
    echo "Signature verification FAILED. The binary may be tampered. Aborting." >&2
    rm -f /tmp/mdm-install /tmp/mdm-install.bundle
    exit 1
  }
  rm -f /tmp/mdm-install.bundle
  echo "Signature verified!"
else
  echo "cosign not found — skipping signature verification."
  echo "Install cosign to verify: https://docs.sigstore.dev/cosign/system_config/installation/"
fi

chmod +x /tmp/mdm-install

mkdir -p "$INSTALL_DIR"

echo "Installing to ${INSTALL_DIR}/mdm..."
if [ -w "$INSTALL_DIR" ]; then
  mv /tmp/mdm-install "${INSTALL_DIR}/mdm"
else
  sudo mv /tmp/mdm-install "${INSTALL_DIR}/mdm"
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
