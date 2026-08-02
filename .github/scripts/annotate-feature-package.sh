#!/usr/bin/env bash
#
# Adds OCI annotations to the published dev container feature manifests, so the
# GHCR package page shows a description instead of "No description provided".
#
# Why this exists at all. `devcontainer features publish` writes exactly two
# manifest annotations — `dev.containers.metadata` (the whole
# devcontainer-feature.json, so a resolver can read the options without pulling
# the tarball) and `com.github.package.type` — and neither the CLI nor
# devcontainers/action has a flag for any others. GHCR reads a package's
# description from `org.opencontainers.image.description`, and its repository
# link from `org.opencontainers.image.source`. Nothing in the publish path sets
# either, which is why every feature published this way, including the ones
# under ghcr.io/devcontainers/features, has a blank description. A feature is an
# OCI artifact rather than an image, so there is no Dockerfile to put a LABEL in;
# annotating the manifest after the push is the only lever.
#
# What it does. For each feature under src/, find the tags pointing at the
# published version, add the annotations to that manifest, and PUT it back under
# every one of those tags.
#
# Two properties worth preserving if this is ever edited:
#
#   1. It is idempotent. Every annotation is derived from
#      devcontainer-feature.json — nothing from the run itself, no `created`
#      timestamp, no commit sha — so a second run computes the same manifest and
#      exits without pushing. That matters because re-pushing a manifest gives
#      it a new digest and leaves the old one behind untagged, so a script that
#      pushed unconditionally would litter the package with a dead version on
#      every commit to main.
#
#   2. It merges rather than replaces. `dev.containers.metadata` is how a
#      resolver reads a feature's options without downloading it, and
#      `com.github.package.type` is what makes GHCR file the package as a dev
#      container feature. Dropping either would break consumers to fix a caption.
#
# Runs from .github/workflows/devcontainer-feature-release.yml after the publish
# step, and can be run by hand against an already-published version:
#
#     GITHUB_TOKEN=<a PAT with write:packages> .github/scripts/annotate-feature-package.sh
#
# tests/devcontainer_feature_test.go pins the workflow wiring and the annotation
# set below.

set -euo pipefail

REGISTRY="ghcr.io"
REPOSITORY="${GITHUB_REPOSITORY:-sethcarney/mdm}"

: "${GITHUB_TOKEN:?GITHUB_TOKEN is required (needs write:packages)}"

if ! command -v jq >/dev/null 2>&1; then
	echo "annotate-feature-package: jq is required" >&2
	exit 1
fi

# Every manifest media type we might be handed back, plus the one we push.
MANIFEST_TYPE="application/vnd.oci.image.manifest.v1+json"

# GHCR issues a short-lived bearer token per repository scope. Basic auth with
# any username and the GITHUB_TOKEN as the password is what it expects; the
# GITHUB_TOKEN itself is not accepted as a bearer token on the /v2 endpoints.
registry_token() {
	local image="$1"
	curl -fsSL -u "x-access-token:${GITHUB_TOKEN}" \
		"https://${REGISTRY}/token?scope=repository:${image}:pull,push&service=${REGISTRY}" |
		jq -r '.token'
}

# Digest a tag currently resolves to, or empty if the tag does not exist. Read
# out of the response header rather than with curl's `%header{}` writer, which
# only exists from curl 8.3 — this script is also meant to be runnable by hand.
tag_digest() {
	local image="$1" tag="$2" token="$3"
	curl -sSI \
		-H "Authorization: Bearer ${token}" \
		-H "Accept: ${MANIFEST_TYPE}" \
		"https://${REGISTRY}/v2/${image}/manifests/${tag}" |
		tr -d '\r' | awk 'tolower($1) == "docker-content-digest:" { print $2 }'
}

annotate_feature() {
	local metadata="$1"
	local id version description documentation name image token

	id=$(jq -r '.id' "$metadata")
	version=$(jq -r '.version' "$metadata")
	name=$(jq -r '.name // .id' "$metadata")
	description=$(jq -r '.description // ""' "$metadata")
	documentation=$(jq -r '.documentationURL // ""' "$metadata")

	if [ -z "$description" ]; then
		echo "annotate-feature-package: ${id} has no description in ${metadata}; nothing to publish" >&2
		return 1
	fi

	# devcontainers/action defaults the namespace to owner/repo, so a feature
	# with id `mdm` in sethcarney/mdm publishes to ghcr.io/sethcarney/mdm/mdm.
	image="${REPOSITORY}/${id}"
	token=$(registry_token "$image")

	local digest
	digest=$(tag_digest "$image" "$version" "$token")
	if [ -z "$digest" ]; then
		echo "annotate-feature-package: ${image}:${version} is not published yet; skipping" >&2
		return 0
	fi

	# Collect the tags pointing at this digest BEFORE pushing anything. The
	# publish step writes :1, :1.0, :1.0.0 and :latest, but only moves the
	# floating ones when the new version is the highest — so which of them
	# belong to this version is a question for the registry, not for semver
	# arithmetic here. The first push also changes the digest, which is why the
	# whole set has to be resolved up front.
	local tags=()
	local tag
	while IFS= read -r tag; do
		[ -n "$tag" ] || continue
		if [ "$(tag_digest "$image" "$tag" "$token")" = "$digest" ]; then
			tags+=("$tag")
		fi
	done < <(curl -fsSL -H "Authorization: Bearer ${token}" \
		"https://${REGISTRY}/v2/${image}/tags/list" | jq -r '.tags[]?')

	local manifest annotated
	manifest=$(curl -fsSL -H "Authorization: Bearer ${token}" -H "Accept: ${MANIFEST_TYPE}" \
		"https://${REGISTRY}/v2/${image}/manifests/${version}")

	# Merging into `.annotations` rather than assigning it keeps
	# `dev.containers.metadata` and `com.github.package.type` — see property 2
	# in the header comment.
	#
	# `image.description` is the one GHCR shows as the caption and the only one
	# strictly needed here; the rest come along because they are the same three
	# lines of jq and they populate the package sidebar. `image.source` also
	# links the package to this repository, which is what lets GHCR show the
	# repo's README on the package page.
	#
	# `licenses` is an SPDX expression, not a URL — the feature's `licenseURL`
	# has no OCI equivalent and is left in the feature metadata, where the CLI
	# already surfaces it. Hardcoded to match the repository's LICENSE.
	annotated=$(jq -Sc --arg description "$description" \
		--arg source "https://github.com/${REPOSITORY}" \
		--arg url "https://github.com/${REPOSITORY}" \
		--arg documentation "${documentation:-https://github.com/${REPOSITORY}}" \
		--arg title "$name" \
		--arg version "$version" \
		--arg licenses "Apache-2.0" \
		'.annotations = (.annotations // {}) * {
			"org.opencontainers.image.description": $description,
			"org.opencontainers.image.source": $source,
			"org.opencontainers.image.url": $url,
			"org.opencontainers.image.documentation": $documentation,
			"org.opencontainers.image.title": $title,
			"org.opencontainers.image.version": $version,
			"org.opencontainers.image.licenses": $licenses
		}' \
		<<<"$manifest")

	if [ "$(jq -Sc . <<<"$manifest")" = "$annotated" ]; then
		echo "annotate-feature-package: ${image}:${version} already annotated (${digest})"
		return 0
	fi

	if [ "${#tags[@]}" -eq 0 ]; then
		echo "annotate-feature-package: no tag resolves to ${digest}; nothing to annotate" >&2
		return 1
	fi

	for tag in "${tags[@]}"; do
		echo "annotate-feature-package: annotating ${image}:${tag}"
		curl -fsSL -X PUT \
			-H "Authorization: Bearer ${token}" \
			-H "Content-Type: ${MANIFEST_TYPE}" \
			--data-binary "$annotated" \
			-o /dev/null \
			"https://${REGISTRY}/v2/${image}/manifests/${tag}"
	done

	echo "annotate-feature-package: ${image} annotated on ${#tags[@]} tag(s): ${tags[*]}"
}

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
for metadata in "$root"/src/*/devcontainer-feature.json; do
	[ -f "$metadata" ] || continue
	annotate_feature "$metadata"
done
