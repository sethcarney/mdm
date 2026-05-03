#!/usr/bin/env bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOCAL_BIN="${HOME}/.local/bin"

echo "Building mdm..."
cd "$SCRIPT_DIR"
make build

mkdir -p "$LOCAL_BIN"
cp mdm "$LOCAL_BIN/mdm"

echo "Installed mdm to $LOCAL_BIN/mdm"

# Warn if LOCAL_BIN is not in PATH
if [[ ":$PATH:" != *":$LOCAL_BIN:"* ]]; then
  echo "Warning: $LOCAL_BIN is not in your PATH. Add the following to your shell config:"
  echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
fi
