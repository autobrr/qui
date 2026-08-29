#!/usr/bin/env bash
#
# Hardened write helper for the discussion triage workflow.
#
# Claude is given ONLY this script for GitHub mutations (no raw `gh api graphql`),
# so a prompt-injection in untrusted discussion content cannot run arbitrary
# GraphQL within the token's `discussions: write` scope. Two invariants hold even
# if Claude is fully compromised:
#
#   1. The target discussion is read from the workflow event payload, never from
#      Claude, so writes can only ever land on the discussion under triage.
#   2. Label names are validated against the repository's real label set before
#      any mutation runs; unknown names are dropped.
#
# Usage (the discussion number is bound to the triggering event, not passed in):
#   ./.github/scripts/discussion-write.sh add-label    <name> [<name>...]
#   ./.github/scripts/discussion-write.sh remove-label <name> [<name>...]
#   ./.github/scripts/discussion-write.sh comment       <body>
#   ./.github/scripts/discussion-write.sh close         <RESOLVED|OUTDATED|DUPLICATE>
#   ./.github/scripts/discussion-write.sh create-issue  <title> <body>
#
# `create-issue` opens a repository issue labelled `ready-for-agent` and appends
# a `From discussion #N` line to the body, where N is the bound discussion.
#
# The calling workflow restricts which of those operations are permitted via
# DISCUSSION_WRITE_ALLOWED_OPS (comma-separated). It defaults to label
# operations so a workflow that forgets to set it still cannot post comments,
# close discussions, or open issues.
#
set -euo pipefail

die() { echo "Error: $*" >&2; exit 1; }

ALLOWED_OPS="${DISCUSSION_WRITE_ALLOWED_OPS:-add-label,remove-label}"
op_allowed() {
  case ",${ALLOWED_OPS}," in
    *",$1,"*) return 0 ;;
    *) return 1 ;;
  esac
}

# --- Resolve the target discussion from the event payload (not from Claude) ---
[[ -n "${GITHUB_EVENT_PATH:-}" && -f "$GITHUB_EVENT_PATH" ]] || die "GITHUB_EVENT_PATH not available"
NUMBER=$(jq -r '.inputs.discussion_number // .discussion.number // empty' "$GITHUB_EVENT_PATH")
[[ "$NUMBER" =~ ^[0-9]+$ ]] || die "no valid discussion number in event payload"

REPO="${GH_REPO:-${GITHUB_REPOSITORY:-}}"
[[ "$REPO" == */* && "$REPO" != */*/* ]] || die "GITHUB_REPOSITORY must be owner/repo"
OWNER="${REPO%%/*}"
NAME="${REPO##*/}"
export GH_REPO="$REPO"

# Resolve the discussion's GraphQL node id. All inputs are passed as typed
# variables, never interpolated into the query string.
discussion_id() {
  # SC2016: the $-prefixed names are GraphQL variables, not shell vars; they must
  # reach gh literally, so single quotes (no shell expansion) are intentional.
  # shellcheck disable=SC2016
  gh api graphql \
    -f query='query($owner:String!,$name:String!,$num:Int!){repository(owner:$owner,name:$name){discussion(number:$num){id}}}' \
    -f owner="$OWNER" -f name="$NAME" -F num="$NUMBER" \
    --jq '.data.repository.discussion.id'
}

# Map requested label NAMES to their ids, dropping any name not present in the
# repository. Prints one id per valid label to stdout.
resolve_label_ids() {
  local labels_json name id
  labels_json=$(gh label list --json name,id --limit 500)
  for name in "$@"; do
    id=$(jq -r --arg n "$name" '.[] | select(.name == $n) | .id' <<<"$labels_json")
    if [[ -n "$id" ]]; then
      printf '%s\n' "$id"
    else
      echo "Skipping unknown label: $name" >&2
    fi
  done
}

apply_labels() {
  local mutation="$1"; shift # addLabelsToLabelable | removeLabelsFromLabelable
  [[ $# -gt 0 ]] || die "no label names given"

  local ids_raw ids=()
  ids_raw=$(resolve_label_ids "$@")
  [[ -n "$ids_raw" ]] && mapfile -t ids <<<"$ids_raw"
  if [[ ${#ids[@]} -eq 0 ]]; then
    echo "No valid labels to apply." >&2
    return 0
  fi

  local node_id var_args=() id query
  node_id=$(discussion_id)
  [[ -n "$node_id" ]] || die "could not resolve discussion #$NUMBER"
  for id in "${ids[@]}"; do var_args+=(-f "labels[]=$id"); done

  # shellcheck disable=SC2016 # $id/$labels are GraphQL variables, not shell vars
  query=$(printf 'mutation($id:ID!,$labels:[ID!]!){%s(input:{labelableId:$id,labelIds:$labels}){clientMutationId}}' "$mutation")
  gh api graphql -f query="$query" -f id="$node_id" "${var_args[@]}" >/dev/null
  echo "${mutation}: ${ids[*]}"
}

cmd="${1:-}"; shift || true
case "$cmd" in
  add-label)
    op_allowed add-label || die "add-label is not permitted here (allowed: $ALLOWED_OPS)"
    apply_labels addLabelsToLabelable "$@"
    ;;
  remove-label)
    op_allowed remove-label || die "remove-label is not permitted here (allowed: $ALLOWED_OPS)"
    apply_labels removeLabelsFromLabelable "$@"
    ;;
  comment)
    op_allowed comment || die "comment is not permitted here (allowed: $ALLOWED_OPS)"
    [[ $# -eq 1 ]] || die "comment requires exactly one body argument"
    [[ -n "$1" ]] || die "comment body is empty"
    node_id=$(discussion_id)
    [[ -n "$node_id" ]] || die "could not resolve discussion #$NUMBER"
    # shellcheck disable=SC2016 # $id/$body are GraphQL variables, not shell vars
    gh api graphql \
      -f query='mutation($id:ID!,$body:String!){addDiscussionComment(input:{discussionId:$id,body:$body}){comment{url}}}' \
      -f id="$node_id" -f body="$1" --jq '.data.addDiscussionComment.comment.url'
    ;;
  close)
    op_allowed close || die "close is not permitted here (allowed: $ALLOWED_OPS)"
    [[ $# -eq 1 ]] || die "close requires exactly one reason argument"
    case "$1" in
      RESOLVED|OUTDATED|DUPLICATE) ;;
      *) die "close reason must be RESOLVED, OUTDATED, or DUPLICATE" ;;
    esac
    node_id=$(discussion_id)
    [[ -n "$node_id" ]] || die "could not resolve discussion #$NUMBER"
    # shellcheck disable=SC2016 # $id/$reason are GraphQL variables, not shell vars
    gh api graphql \
      -f query='mutation($id:ID!,$reason:DiscussionCloseReason!){closeDiscussion(input:{discussionId:$id,reason:$reason}){discussion{url}}}' \
      -f id="$node_id" -f reason="$1" --jq '.data.closeDiscussion.discussion.url'
    ;;
  create-issue)
    op_allowed create-issue || die "create-issue is not permitted here (allowed: $ALLOWED_OPS)"
    [[ $# -eq 2 ]] || die "create-issue requires a title and a body argument"
    [[ -n "$1" && -n "$2" ]] || die "create-issue title and body must not be empty"
    # A re-run on an already promoted discussion reuses its issue.
    # ponytail: issue search is eventually consistent, so this is a backstop; the
    # playbook's closed/label check is the real guard.
    existing=$(gh issue list --state all --search "\"From discussion #$NUMBER\" in:body author:app/github-actions" --json url --jq '.[0].url // empty')
    if [[ -n "$existing" ]]; then
      echo "Issue already exists for discussion #$NUMBER: $existing" >&2
      echo "$existing"
      exit 0
    fi
    gh issue create --title "$1" --label ready-for-agent \
      --body "$2"$'\n\n'"From discussion #$NUMBER"
    ;;
  *)
    die "unknown command '$cmd' (use: add-label | remove-label | comment | close | create-issue)"
    ;;
esac
