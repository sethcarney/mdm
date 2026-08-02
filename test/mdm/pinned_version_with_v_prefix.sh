#!/usr/bin/env bash
#
# Same release as pinned_version, written the way the git tag is. Release tags
# carry a leading v and release *numbers* usually do not, so the option accepts
# either spelling and normalises to the tag.

set -e

source dev-container-features-test-lib

check "installed the pinned release" bash -c "mdm --version | grep -F '1.9.1'"

reportResults
