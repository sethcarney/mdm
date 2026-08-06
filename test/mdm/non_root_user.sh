#!/usr/bin/env bash
#
# The reason the binary goes to /usr/local/bin rather than a home directory:
# it has to work for the remoteUser, whoever that turns out to be.
#
# And the reason /usr/local/bin holds a symlink rather than the binary: the
# remoteUser has to be able to replace it, or `mdm upgrade` fails with
# "permission denied" and an image rebuild becomes the only way to take a new
# release.

set -e

source dev-container-features-test-lib

check "test runs as a non-root user" bash -c '[ "$(id -u)" -ne 0 ]'
check "non-root user can run mdm" bash -c "mdm --version"

# /usr/local/bin itself stays root-owned: the point is to make mdm upgradable,
# not to hand the user every other feature's binaries.
check "non-root user cannot write /usr/local/bin" bash -c "! test -w /usr/local/bin"
check "mdm on PATH is a symlink" bash -c "test -L /usr/local/bin/mdm"

# What `mdm upgrade` actually does: unlink the old binary and put a new one in
# its place. That is a permission on the directory, so this replaces the binary
# with a copy of itself — as the remoteUser, with no sudo anywhere.
check "non-root user can replace the binary" bash -c '
  real="$(readlink -f /usr/local/bin/mdm)"
  cp "$real" /tmp/mdm-copy
  rm "$real"
  cp /tmp/mdm-copy "$real"
  chmod 0755 "$real"
  rm -f /tmp/mdm-copy
  mdm --version'

reportResults
