#!/usr/bin/env bash
#
# The reason the binary goes to /usr/local/bin rather than a home directory:
# it has to work for the remoteUser, whoever that turns out to be, without
# being writable by them.

set -e

source dev-container-features-test-lib

check "test runs as a non-root user" bash -c '[ "$(id -u)" -ne 0 ]'
check "non-root user can run mdm" bash -c "mdm --version"
check "non-root user cannot overwrite mdm" bash -c "! test -w /usr/local/bin/mdm"

reportResults
