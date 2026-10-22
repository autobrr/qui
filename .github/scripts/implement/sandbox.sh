#!/usr/bin/env bash
# Runs one command for the implementer agent in a container. The container has
# no network and no secrets, and it cannot see the processes of the job. The
# repo is read-only in the container, so the command cannot change a file that
# Claude Code or a later step runs. Only web/node_modules is writable, for the
# caches of tsc and vitest. The workflow installs this script outside the repo.
# See docs/adr/0021-the-implementer-runs-on-the-maintainers-subscription.md.
set -euo pipefail

repo=$GITHUB_WORKSPACE
exec docker run --rm --network none --cap-drop ALL --security-opt no-new-privileges \
  --user "$(id -u):$(id -g)" \
  -v "$repo:$repo:ro" \
  -v "$repo/web/node_modules:$repo/web/node_modules" \
  -v /opt/hostedtoolcache:/opt/hostedtoolcache:ro \
  -v "$HOME/setup-pnpm:$HOME/setup-pnpm:ro" \
  -v "$GOMODCACHE:$GOMODCACHE:ro" \
  -v "$GOCACHE:$GOCACHE" \
  -e PATH -e GOMODCACHE -e GOCACHE -e HOME=/tmp \
  -e GOTOOLCHAIN=local -e GOPROXY=off -e GOFLAGS=-mod=readonly \
  -w "$repo" ubuntu:24.04 "$@"
