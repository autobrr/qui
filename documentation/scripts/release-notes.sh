#!/usr/bin/env bash
# Writes the release notes page for one GitHub release to documentation/blog.
# Usage: release-notes.sh <tag>
# Needs gh with read access to autobrr/qui.
set -euo pipefail

tag=$1
repo=autobrr/qui
blog_dir="$(dirname "$0")/../blog"

IFS=$'\t' read -r date prerelease < <(
	gh release view "$tag" -R "$repo" --json publishedAt,isPrerelease \
		--jq '[.publishedAt[:10], .isPrerelease] | @tsv'
)
if [[ $prerelease == true ]]; then
	echo "skip $tag: prerelease"
	exit 0
fi

mkdir -p "$blog_dir"
out="$blog_dir/$date-$tag.md"

{
	# format: md stops MDX from reading "<" and "{" in commit messages as JSX.
	printf -- '---\ntitle: %s\nslug: %s\ndate: %s\nmdx:\n  format: md\n---\n\n' "$tag" "$tag" "$date"
	gh release view "$tag" -R "$repo" --json body --jq .body |
		tr -d '\r' |
		# Drop the GoReleaser footer (Docker images, Discord link).
		sed '/^## Docker images/,$d' |
		# GoReleaser writes "## Changelog" at the top, and Highlights get added
		# below it by hand. Move the heading to the first change list instead.
		awk '
			/^## Changelog$/ { next }
			!moved && /^### (Breaking Changes|New Features|Bug Fixes|Other Changes)$/ { print "## Changelog\n"; moved = 1 }
			{ print }
		' |
		sed -E \
			-e 's/^\* [0-9a-f]{40}: /* /' \
			-e "s|\(#([0-9]+)\)|([#\1](https://github.com/$repo/pull/\1))|g" \
			-e 's|\(@([A-Za-z0-9-]+)\)|([@\1](https://github.com/\1))|g'
} >"$out"

echo "wrote $out"
