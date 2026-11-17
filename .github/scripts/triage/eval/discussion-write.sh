#!/usr/bin/env bash
# Stand-in for .github/scripts/discussion-write.sh in a triage eval run. It
# checks the arguments the way the real script does, appends each operation
# to $QUI_EVAL_WRITES as one JSON line, and writes nothing to GitHub.
# shellcheck disable=SC2016 # $names in the jq programs are jq variables
set -euo pipefail

die() { echo "Error: $*" >&2; exit 1; }

read_body() {
  body=$(cat)
  [[ -n "$body" ]] || die "$1 body is empty: pass it on stdin with <<'EOF'"
}

ALLOWED_OPS="${DISCUSSION_WRITE_ALLOWED_OPS:-add-label,remove-label}"
op_allowed() {
  case ",${ALLOWED_OPS}," in
    *",$1,"*) return 0 ;;
    *) return 1 ;;
  esac
}

[[ -n "${QUI_EVAL_WRITES:-}" && -n "${QUI_EVAL_DISCUSSION:-}" ]] || die "GITHUB_EVENT_PATH not available"
NUMBER=$QUI_EVAL_DISCUSSION

record() { jq -cn "$@" >>"$QUI_EVAL_WRITES"; }

valid_labels() {
  local known name
  known=$(gh label list --json name --limit 500 --jq '.[].name')
  for name in "$@"; do
    if grep -qxF -- "$name" <<<"$known"; then
      printf '%s\n' "$name"
    else
      echo "Skipping unknown label: $name" >&2
    fi
  done
}

apply_labels() {
  local op=$1 mutation=$2; shift 2
  [[ $# -gt 0 ]] || die "no label names given"
  local names=()
  mapfile -t names < <(valid_labels "$@")
  if [[ ${#names[@]} -eq 0 ]]; then
    echo "No valid labels to apply." >&2
    return 0
  fi
  record --arg op "$op" '{op: $op, labels: $ARGS.positional}' --args "${names[@]}"
  echo "${mutation}: ${names[*]}"
}

cmd="${1:-}"; shift || true
case "$cmd" in
  add-label)
    op_allowed add-label || die "add-label is not permitted here (allowed: $ALLOWED_OPS)"
    apply_labels add-label addLabelsToLabelable "$@"
    ;;
  remove-label)
    op_allowed remove-label || die "remove-label is not permitted here (allowed: $ALLOWED_OPS)"
    apply_labels remove-label removeLabelsFromLabelable "$@"
    ;;
  comment)
    op_allowed comment || die "comment is not permitted here (allowed: $ALLOWED_OPS)"
    [[ $# -eq 0 ]] || die "comment takes no arguments: pass the body on stdin with <<'EOF'"
    read_body comment
    record --arg body "$body" '{op: "comment", body: $body}'
    echo "https://github.com/autobrr/qui/discussions/$NUMBER#discussioncomment-1"
    ;;
  close)
    op_allowed close || die "close is not permitted here (allowed: $ALLOWED_OPS)"
    [[ $# -eq 1 ]] || die "close requires exactly one reason argument"
    case "$1" in
      RESOLVED|OUTDATED|DUPLICATE) ;;
      *) die "close reason must be RESOLVED, OUTDATED, or DUPLICATE" ;;
    esac
    record --arg reason "$1" '{op: "close", reason: $reason}'
    echo "https://github.com/autobrr/qui/discussions/$NUMBER"
    ;;
  create-issue)
    op_allowed create-issue || die "create-issue is not permitted here (allowed: $ALLOWED_OPS)"
    [[ $# -ge 2 ]] || die "create-issue requires a state and a title, and the body on stdin"
    state=$1 title=$2; shift 2
    [[ "$state" == ready-for-agent || "$state" == ready-for-human ]] || die "create-issue state must be ready-for-agent or ready-for-human"
    [[ -n "$title" ]] || die "create-issue title must not be empty"
    read_body create-issue
    record --arg state "$state" --arg title "$title" --arg body "$body"$'\n\n'"From discussion #$NUMBER" \
      '{op: "create-issue", state: $state, title: $title, body: $body, labels: $ARGS.positional}' --args "$@"
    echo "https://github.com/autobrr/qui/issues/99999"
    ;;
  *)
    die "unknown command '$cmd' (use: add-label | remove-label | comment | close | create-issue)"
    ;;
esac
