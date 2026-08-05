# Base layer for the mdm dev container.
#
# This file exists for exactly one reason: to make the feature install chain
# survive rootless podman. Everything else about the container is configured in
# devcontainer.json.
#
# The failure it fixes
# -------------------
# Before invoking gpgv, apt drops privileges to the unprivileged `_apt` user and
# writes a temporary config file into /tmp. Under rootless podman that write is
# refused part-way through the build, and every apt-using feature installed after
# that point dies with:
#
#   Couldn't create temporary file /tmp/apt.conf.XXXXXX for passing config to apt-key
#   E: The repository 'http://deb.debian.org/debian bookworm InRelease' is not signed.
#   ERROR: Feature "Go" (ghcr.io/devcontainers/features/go) failed to install!
#
# The "not signed" line is a red herring — apt is not rejecting a bad signature,
# it never managed to check one. The lone cause is the refused write to /tmp.
#
# Why this fix and not a chmod alone
# ----------------------------------
# The chmod restores /tmp to the conventional 1777, but it only holds until
# whatever step is resetting it runs again. Features install in sequence and any
# of them can disturb /tmp, so a fix that depends on /tmp's mode is a fix that
# depends on install order. Setting APT::Sandbox::User to root stops apt from
# dropping privileges at all, so the mode of /tmp stops mattering for the rest of
# the build no matter which feature runs when.
#
# What it costs
# -------------
# apt's sandbox is defence-in-depth: it limits what a malicious package server
# could do during the download-and-verify step. It is not a boundary this build
# relies on — the build already runs as root, and the result is a development
# container, not a published artifact. Under Docker the setting is inert, since
# the sandbox was never failing there.
#
# If podman is not in play, this whole layer can be dropped and devcontainer.json
# can go back to a plain "image" key.

ARG BASE_IMAGE=mcr.microsoft.com/devcontainers/base:bookworm
FROM ${BASE_IMAGE}

RUN chmod 1777 /tmp \
    && printf 'APT::Sandbox::User "root";\n' \
        > /etc/apt/apt.conf.d/99-devcontainer-apt-no-sandbox
