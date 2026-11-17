#!/usr/bin/env bash
# Print one qBittorrent source file at a release tag. `gh search code` reads
# only the newest code, so triage uses this for the version a reporter runs.
# The triage agent reads untrusted text, so it gets this script, not `gh api`.
set -euo pipefail

if [[ $# -ne 2 || ! $1 =~ ^release-[0-9]+\.[0-9]+\.[0-9]+$ || ! $2 =~ ^[A-Za-z0-9_][A-Za-z0-9_./-]*$ || $2 == *..* ]]; then
  echo "usage: qbit-source.sh release-<major.minor.patch> <path>" >&2
  exit 2
fi

exec gh api "repos/qbittorrent/qBittorrent/contents/$2" -X GET -f ref="$1" -H "Accept: application/vnd.github.raw"
