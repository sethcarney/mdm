#!/usr/bin/env bash
#
# `version` set to a bare release number: the feature must install exactly that
# release, not the latest one.

set -e

source dev-container-features-test-lib

check "mdm is on PATH" bash -c "command -v mdm"
check "installed the pinned release" bash -c "mdm --version | grep -F '1.9.1'"

reportResults
