#!/usr/bin/env bash
# build.sh <commit>: build qui at <commit> once, keep the binary, and print its path.
# Builds from a git archive, so no worktree is added. Offline: pnpm reads only its store.
set -euo pipefail
REPO=$(git -C "$(dirname "$0")" rev-parse --show-toplevel)
SHA=$(git -C "$REPO" rev-parse --verify "$1^{commit}")
CACHE=${QUI_PROVE_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/qui-prove}
BIN=$CACHE/bin/$SHA/qui
mkdir -p "$CACHE/bin/$SHA"
exec 9>"$CACHE/bin/$SHA/lock"
flock 9
if [ -x "$BIN" ]; then echo "$BIN"; exit 0; fi
SRC=$CACHE/src/$SHA
rm -rf "$SRC"
mkdir -p "$SRC"
git -C "$REPO" archive "$SHA" | tar -x -C "$SRC"
{
  (cd "$SRC/web" && pnpm install --frozen-lockfile --offline && pnpm build)
  PKG=github.com/autobrr/qui/internal/buildinfo
  (cd "$SRC" && go build -ldflags "-X $PKG.Version=prove-${SHA:0:9} -X $PKG.Commit=$SHA" -o "$BIN.tmp" ./cmd/qui)
} > "$CACHE/bin/$SHA/build.log" 2>&1 || { tail -20 "$CACHE/bin/$SHA/build.log" >&2; exit 1; }
mv "$BIN.tmp" "$BIN"
rm -rf "$SRC"
echo "$BIN"
