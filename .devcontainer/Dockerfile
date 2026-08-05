# Base image for the dev container, wrapping the stock Debian base only to
# pre-install the apt packages the features below would otherwise install
# themselves.
#
# Why: under podman, a buildah bug (containers/buildah#6503) corrupts /tmp in
# the image layer committed by every feature-install step — each of those steps
# bind-mounts under /tmp, and the committed layer loses /tmp's 1777 mode. The
# first feature builds against a healthy /tmp; any later feature that runs
# `apt-get update` fails with
#
#   Couldn't create temporary file /tmp/apt.conf.XXXXXX for passing config to apt-key
#
# because apt's privilege-separated helper can no longer write to /tmp, and the
# build dies with "repository is not signed" for every source.
#
# Every feature this repo uses skips apt when nothing it needs is missing, so
# installing their package lists here — an ordinary build step that runs before
# any feature bind mount exists — keeps the feature steps apt-free. Under
# docker this is a harmless no-op (the packages would have been installed
# anyway). post-create.sh repairs /tmp's mode for the running container.
FROM mcr.microsoft.com/devcontainers/base:bookworm

# The full check_packages list of ghcr.io/devcontainers/features/go. git is
# already in the base image, but it is load-bearing twice: the go feature
# checks for it, and its presence is what makes the git feature ("os-provided")
# exit before ever touching apt. gnupg2 pulls in gnupg, which the claude-code
# feature checks for.
RUN apt-get update \
    && export DEBIAN_FRONTEND=noninteractive \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        g++ \
        gcc \
        git \
        gnupg2 \
        libc6-dev \
        make \
        pkg-config \
        tar \
    && rm -rf /var/lib/apt/lists/*
