#!/bin/sh
set -eu

base=$(git merge-base origin/develop HEAD 2>/dev/null || true)
if [ -z "$base" ]; then
	echo "prepush: no merge base with origin/develop. Run: git fetch origin develop" >&2
	exit 1
fi

changed=$({ git -c core.quotePath=false diff --no-renames --name-only "$base"; git -c core.quotePath=false ls-files --others --exclude-standard; } | sort -u)
if [ -z "$changed" ]; then
	echo "prepush: no changed file since the origin/develop merge base."
	exit 0
fi

lint_workflow() {
	if ! command -v actionlint >/dev/null 2>&1; then
		echo "prepush: a workflow changed, and actionlint is not installed. Install it: go install github.com/rhysd/actionlint/cmd/actionlint@latest" >&2
		return 1
	fi
	actionlint "$1"
}

printf '%s\n' "$changed" | while read -r file; do
	[ -f "$file" ] || continue
	case "$file" in
	.github/workflows/*.yml | .github/workflows/*.yaml) lint_workflow "$file" ;;
	*.sh) bash -n "$file" ;;
	documentation/*.js) ;;
	*.js | *.mjs | *.cjs) node --check "$file" ;;
	esac
done
if printf '%s\n' "$changed" | grep -q '^\.github/scripts/'; then
	python3 -m unittest discover -s .github/scripts -p 'test_*.py'
fi

if ! printf '%s\n' "$changed" | grep -qvE '^\.github/|^scripts/.*\.(sh|py|md|txt)$'; then
	make check-mojibake
	echo "prepush: passed. Only .github/ and script files changed, so precommit, the Go tests, and make build are not needed."
	exit 0
fi

make precommit
packages=$(printf '%s\n' "$changed" | grep '\.go$' | while read -r file; do
	dir=$(dirname "$file")
	if [ -d "$dir" ] && go list "./$dir" >/dev/null 2>&1; then echo "./$dir"; fi
done | sort -u)
if [ -n "$packages" ]; then
	echo "prepush: testing the changed Go packages..."
	# shellcheck disable=SC2086
	go test -count=1 $packages
fi
echo "prepush: passed. Before the final report, also run: make build"
