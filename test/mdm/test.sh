#!/usr/bin/env bash
#
# Default test for the mdm feature: installed with no options, so `version`
# takes its default of "latest".
#
# The devcontainer CLI runs this one against every base image in the test
# matrix, as whatever user that image defaults to — so it asserts only what
# holds everywhere. User-specific and option-specific behaviour lives in
# scenarios.json alongside this file.
#
# Run with:
#   devcontainer features test -f mdm -i mcr.microsoft.com/devcontainers/base:ubuntu .

set -e

# Provides `check` and `reportResults`; injected into the container by the
# devcontainer CLI test harness.
source dev-container-features-test-lib

check "mdm is on PATH" bash -c "command -v mdm"
check "mdm reports a version" bash -c "mdm --version"
check "mdm --help works" bash -c "mdm --help"

# The install location is the point of the feature: /usr/local/bin is on PATH
# for every user, so the binary works no matter which remoteUser the image runs
# as. ~/.local/bin — where the curl installer puts it — would not.
check "installed to /usr/local/bin" bash -c "test -x /usr/local/bin/mdm"
check "resolves to /usr/local/bin/mdm" bash -c '[ "$(command -v mdm)" = /usr/local/bin/mdm ]'

# Exercises a real subcommand rather than just the version banner.
check "lists agents" bash -c "mdm agents list"

reportResults
