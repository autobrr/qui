#!/usr/bin/env bash
# sync-upstream.sh — rebase our 2 custom commits onto upstream/develop,
# automatically renaming migration files if upstream added new migrations
# that conflict with our numbers.
#
# Safe to run locally or from GitHub Actions.
# Exits 0 on success, 1 on unresolvable conflicts (GH Actions then opens an issue).
set -euo pipefail

UPSTREAM_REMOTE="${UPSTREAM_REMOTE:-upstream}"
UPSTREAM_BRANCH="${UPSTREAM_BRANCH:-develop}"
LOCAL_BRANCH="${LOCAL_BRANCH:-develop}"
SQLITE_MIGRATION_DIR="internal/database/migrations"
PG_MIGRATION_DIR="internal/database/postgres_migrations"
MIGRATION_BASENAME="add_tracker_category_settings.sql"

# ── helpers ──────────────────────────────────────────────────────────────────

info() { printf 'INFO  %s\n' "$*"; }
ok()   { printf 'OK    %s\n' "$*"; }
fail() { printf 'ERROR %s\n' "$*" >&2; exit 1; }

# Return the highest N from files named NNN_*.sql in a directory.
# With a remote arg, lists from the git tree instead of the filesystem.
highest_migration_number() {
  local dir="$1" remote="${2:-}"
  local nums
  if [[ -n "$remote" ]]; then
    # List files from the remote git tree, extract leading number from basename
    nums=$(git ls-tree -r --name-only "${remote}/${UPSTREAM_BRANCH}" -- "$dir" 2>/dev/null \
      | sed 's|.*/||'       \
      | grep -E '^[0-9]+_'  \
      | sed 's/^\([0-9]*\)_.*/\1/' \
      || true)
  else
    nums=$(find "$dir" -maxdepth 1 -name '*.sql' 2>/dev/null \
      | sed 's|.*/||'       \
      | grep -E '^[0-9]+_'  \
      | sed 's/^\([0-9]*\)_.*/\1/' \
      || true)
  fi
  if [[ -z "$nums" ]]; then
    echo "0"
  else
    echo "$nums" | sort -n | tail -1
  fi
}

# Resolve all currently conflicted migration-only files during an active rebase.
# Takes our version (--theirs in rebase = our replayed commit's content),
# renames to $1 (sqlite target num) / $2 (pg target num).
resolve_migration_conflicts() {
  local sqlite_num="$1" pg_num="$2"
  local conflicted
  conflicted=$(git diff --name-only --diff-filter=U 2>/dev/null || true)

  if [[ -z "$conflicted" ]]; then
    ok "No conflicts to resolve."
    return 0
  fi

  # Bail if anything outside migration dirs is conflicted
  local non_migration
  non_migration=$(printf '%s\n' "$conflicted" \
    | grep -v -E '^(internal/database/migrations|internal/database/postgres_migrations)/' \
    || true)
  if [[ -n "$non_migration" ]]; then
    git rebase --abort 2>/dev/null || true
    fail "Non-migration conflicts detected — manual rebase required:
${non_migration}"
  fi

  info "Resolving migration conflicts..."
  while IFS= read -r fpath; do
    [[ -z "$fpath" ]] && continue
    local dir bname suffix new_num new_name new_path
    dir=$(dirname "$fpath")
    bname=$(basename "$fpath")
    suffix="${bname#*_}"  # e.g. "add_tracker_category_settings.sql"

    if [[ "$dir" == *"postgres_migrations"* ]]; then
      new_num="$pg_num"
    else
      new_num="$sqlite_num"
    fi

    new_name="${new_num}_${suffix}"
    new_path="${dir}/${new_name}"

    # Take our version of the file content
    git checkout --theirs -- "$fpath"

    if [[ "$bname" != "$new_name" ]]; then
      info "  Renaming $bname -> $new_name"
      git rm --cached -- "$fpath"
      mv "$fpath" "$new_path"
      git add -- "$new_path"
    else
      git add -- "$fpath"
    fi
  done <<< "$conflicted"
}

# Rename migration files on disk and stage the change.
rename_migration_if_needed() {
  local dir="$1" old_num="$2" new_num="$3"
  [[ "$old_num" == "$new_num" ]] && return 0

  local old_path new_path
  old_path=$(find "$dir" -maxdepth 1 -name "${old_num}_${MIGRATION_BASENAME}" | head -1 || true)
  if [[ -z "$old_path" ]]; then
    # Maybe it was already renamed (e.g. during conflict resolution)
    old_path=$(find "$dir" -maxdepth 1 -name "*_${MIGRATION_BASENAME}" | head -1 || true)
    if [[ -z "$old_path" ]]; then
      info "  Migration file not found in $dir — nothing to rename."
      return 0
    fi
    local actual_num
    actual_num=$(basename "$old_path" | grep -oE '^[0-9]+')
    [[ "$actual_num" == "$new_num" ]] && { info "  Already at $new_num — no rename needed."; return 0; }
  fi

  new_path="${dir}/${new_num}_${MIGRATION_BASENAME}"
  info "  $dir: $(basename "$old_path") -> $(basename "$new_path")"
  git mv "$old_path" "$new_path"
}

# ── preflight ─────────────────────────────────────────────────────────────────

info "=== sync-upstream.sh ==="
info "Local branch: $LOCAL_BRANCH  Upstream: ${UPSTREAM_REMOTE}/${UPSTREAM_BRANCH}"

current_branch=$(git rev-parse --abbrev-ref HEAD)
[[ "$current_branch" == "$LOCAL_BRANCH" ]] \
  || fail "Expected branch '$LOCAL_BRANCH', currently on '$current_branch'."

[[ -z "$(git status --porcelain)" ]] \
  || fail "Working tree is dirty. Commit or stash changes before syncing."

info "Fetching ${UPSTREAM_REMOTE}..."
git fetch "$UPSTREAM_REMOTE"

merge_base=$(git merge-base HEAD "${UPSTREAM_REMOTE}/${UPSTREAM_BRANCH}")
upstream_head=$(git rev-parse "${UPSTREAM_REMOTE}/${UPSTREAM_BRANCH}")

if [[ "$merge_base" == "$upstream_head" ]]; then
  ok "Already up to date with ${UPSTREAM_REMOTE}/${UPSTREAM_BRANCH}."
  exit 0
fi

new_upstream_count=$(git rev-list --count "${merge_base}..${UPSTREAM_REMOTE}/${UPSTREAM_BRANCH}")
info "${new_upstream_count} new upstream commit(s) to rebase onto."

# ── locate our custom commits ─────────────────────────────────────────────────
# Walk back from HEAD until we find the two cross-seed commits, skipping any
# housekeeping commits (like the sync workflow commit itself).

info "Locating our custom cross-seed commits..."

OUR_COMMIT_PATTERNS="cross-seed|tracker.category|revert.*revert"
OUR_COMMITS_NEEDED=2
our_commit_hashes=()
our_commit_msgs=()

for i in $(seq 0 9); do
  ref="HEAD~${i}"
  msg=$(git log -1 --format="%s" "$ref" 2>/dev/null || true)
  [[ -z "$msg" ]] && break
  # Stop once we hit an upstream commit (has a PR number and no custom pattern)
  if echo "$msg" | grep -qE '\(#[0-9]+\)$' && ! echo "$msg" | grep -qi "$OUR_COMMIT_PATTERNS"; then
    break
  fi
  if echo "$msg" | grep -qi "$OUR_COMMIT_PATTERNS"; then
    our_commit_hashes+=("$(git rev-parse "${ref}")")
    our_commit_msgs+=("$msg")
    info "  found: $(git rev-parse --short ${ref})  $msg"
    [[ "${#our_commit_hashes[@]}" -ge "$OUR_COMMITS_NEEDED" ]] && break
  fi
done

if [[ "${#our_commit_hashes[@]}" -lt "$OUR_COMMITS_NEEDED" ]]; then
  fail "Could not find $OUR_COMMITS_NEEDED custom cross-seed commits in the top 10 commits. Aborting."
fi

# ── determine required migration numbers ──────────────────────────────────────

info "Calculating migration numbers..."

upstream_sqlite_max=$(highest_migration_number "$SQLITE_MIGRATION_DIR" "$UPSTREAM_REMOTE")
upstream_pg_max=$(highest_migration_number "$PG_MIGRATION_DIR" "$UPSTREAM_REMOTE")

needed_sqlite_num=$(printf "%03d" $((upstream_sqlite_max + 1)))
needed_pg_num=$(printf "%03d" $((upstream_pg_max + 1)))

# Current numbers in our working tree (before rebase)
current_sqlite_file=$(find "$SQLITE_MIGRATION_DIR" -maxdepth 1 \
  -name "*_${MIGRATION_BASENAME}" | head -1 || true)
current_pg_file=$(find "$PG_MIGRATION_DIR" -maxdepth 1 \
  -name "*_${MIGRATION_BASENAME}" | head -1 || true)

current_sqlite_num="000"
current_pg_num="000"
[[ -n "$current_sqlite_file" ]] && \
  current_sqlite_num=$(basename "$current_sqlite_file" | grep -oE '^[0-9]+')
[[ -n "$current_pg_file" ]] && \
  current_pg_num=$(basename "$current_pg_file" | grep -oE '^[0-9]+')

info "  SQLite:   upstream max=${upstream_sqlite_max}, ours=${current_sqlite_num}, needed=${needed_sqlite_num}"
info "  Postgres: upstream max=${upstream_pg_max},  ours=${current_pg_num},  needed=${needed_pg_num}"

# ── attempt rebase ────────────────────────────────────────────────────────────

info "Rebasing onto ${UPSTREAM_REMOTE}/${UPSTREAM_BRANCH}..."

rebase_clean=true
git rebase "${UPSTREAM_REMOTE}/${UPSTREAM_BRANCH}" || rebase_clean=false

if [[ "$rebase_clean" == "false" ]]; then
  info "Rebase stopped with conflicts — checking if they are migration-only..."

  # Round 1: resolve conflicts in the first (older) commit
  resolve_migration_conflicts "$needed_sqlite_num" "$needed_pg_num"

  info "Continuing rebase (round 1)..."
  GIT_EDITOR=true git rebase --continue || {
    # Round 2: conflicts in the second (newer) commit
    info "Rebase stopped again — checking round 2 conflicts..."
    resolve_migration_conflicts "$needed_sqlite_num" "$needed_pg_num"
    info "Continuing rebase (round 2)..."
    GIT_EDITOR=true git rebase --continue || {
      git rebase --abort 2>/dev/null || true
      fail "Rebase did not complete after two conflict-resolution rounds. Manual intervention required."
    }
  }

  ok "Migration conflicts resolved and rebase completed."
fi

# ── rename migrations in place if rebase was clean but numbers are stale ──────
#
# If the rebase succeeded without conflict (upstream didn't touch our migration
# files), our files still have the old numbers. We need to rename them and fold
# the change into the commit that added them (HEAD~1 = the older custom commit).

post_sqlite_file=$(find "$SQLITE_MIGRATION_DIR" -maxdepth 1 \
  -name "*_${MIGRATION_BASENAME}" | head -1 || true)
post_pg_file=$(find "$PG_MIGRATION_DIR" -maxdepth 1 \
  -name "*_${MIGRATION_BASENAME}" | head -1 || true)

post_sqlite_num="000"
post_pg_num="000"
[[ -n "$post_sqlite_file" ]] && \
  post_sqlite_num=$(basename "$post_sqlite_file" | grep -oE '^[0-9]+')
[[ -n "$post_pg_file" ]] && \
  post_pg_num=$(basename "$post_pg_file" | grep -oE '^[0-9]+')

sqlite_needs_rename=false
pg_needs_rename=false
[[ "$post_sqlite_num" != "$needed_sqlite_num" ]] && sqlite_needs_rename=true
[[ "$post_pg_num" != "$needed_pg_num" ]] && pg_needs_rename=true

if [[ "$sqlite_needs_rename" == "true" || "$pg_needs_rename" == "true" ]]; then
  info "Migration numbers are stale after clean rebase — renaming..."

  # We need to amend HEAD~1 (the older commit that added the migrations).
  # Strategy:
  #   1. Save the newer commit (HEAD) as a patch
  #   2. Reset to HEAD~1
  #   3. Rename the migration files
  #   4. Amend HEAD~1 (preserving author/date)
  #   5. Cherry-pick the saved newer commit

  newer_hash=$(git rev-parse HEAD)
  newer_author_name=$(git log -1 --format="%an" HEAD)
  newer_author_email=$(git log -1 --format="%ae" HEAD)
  newer_author_date=$(git log -1 --format="%aI" HEAD)
  newer_subject=$(git log -1 --format="%s" HEAD)
  newer_body=$(git log -1 --format="%b" HEAD)

  older_author_name=$(git log -1 --format="%an" HEAD~1)
  older_author_email=$(git log -1 --format="%ae" HEAD~1)
  older_author_date=$(git log -1 --format="%aI" HEAD~1)

  info "Resetting to HEAD~1 to amend the older commit..."
  git reset --soft HEAD~1

  # The working tree is now at the state of the older commit.
  # Rename the migration files (they are already staged from the soft reset).

  # Unstage everything, work from clean slate
  git restore --staged .

  if [[ "$sqlite_needs_rename" == "true" ]]; then
    rename_migration_if_needed "$SQLITE_MIGRATION_DIR" "$post_sqlite_num" "$needed_sqlite_num"
  fi
  if [[ "$pg_needs_rename" == "true" ]]; then
    rename_migration_if_needed "$PG_MIGRATION_DIR" "$post_pg_num" "$needed_pg_num"
  fi

  # Stage everything that was in the older commit (git mv already staged the rename)
  git add -u

  info "Amending older commit with renamed migrations..."
  GIT_AUTHOR_NAME="$older_author_name" \
  GIT_AUTHOR_EMAIL="$older_author_email" \
  GIT_AUTHOR_DATE="$older_author_date" \
  GIT_COMMITTER_NAME="$older_author_name" \
  GIT_COMMITTER_EMAIL="$older_author_email" \
  GIT_COMMITTER_DATE="$older_author_date" \
    git commit --amend --no-edit

  info "Re-applying newer commit..."
  GIT_AUTHOR_NAME="$newer_author_name" \
  GIT_AUTHOR_EMAIL="$newer_author_email" \
  GIT_AUTHOR_DATE="$newer_author_date" \
  GIT_COMMITTER_NAME="$newer_author_name" \
  GIT_COMMITTER_EMAIL="$newer_author_email" \
  GIT_COMMITTER_DATE="$newer_author_date" \
    git cherry-pick "$newer_hash"

  ok "Migration files renamed and commits updated."
else
  info "Migration numbers are correct (${needed_sqlite_num}/${needed_pg_num}) — no rename needed."
fi

# ── ensure committer == author on our top 2 commits ──────────────────────────
#
# After a plain rebase git sets the committer to whoever ran the rebase.
# Fix that so the history stays clean.  We only do this if the commits
# weren't already amended above (where we set both explicitly).

if [[ "$sqlite_needs_rename" == "false" && "$pg_needs_rename" == "false" ]]; then
  info "Fixing committer identity on our 2 commits..."
  FILTER_BRANCH_SQUELCH_WARNING=1 git filter-branch -f --env-filter '
    GIT_COMMITTER_NAME="$GIT_AUTHOR_NAME"
    GIT_COMMITTER_EMAIL="$GIT_AUTHOR_EMAIL"
    GIT_COMMITTER_DATE="$GIT_AUTHOR_DATE"
  ' "HEAD~2..HEAD"
fi

# ── summary ───────────────────────────────────────────────────────────────────

ok "=== Rebase complete ==="
info "Top 2 commits:"
git log --oneline -2
