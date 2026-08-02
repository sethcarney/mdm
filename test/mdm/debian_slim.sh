#!/usr/bin/env bash
#
# A slim base image ships without curl and without the CA bundle, so this
# scenario covers the dependency bootstrap in install.sh. If that path breaks,
# the feature fails at build time on exactly the minimal images people reach for.

set -e

source dev-container-features-test-lib

check "mdm is on PATH" bash -c "command -v mdm"
check "mdm reports a version" bash -c "mdm --version"

# install.sh clears the apt lists it populated, so the package indexes it had to
# download do not ride along in the image layer.
check "apt package indexes cleaned up" bash -c '! ls /var/lib/apt/lists/*_* >/dev/null 2>&1'

reportResults
