#!/usr/bin/env bash
# Writes the Breaking changes and Highlights sections of a GitHub release with
# Claude, inserts them under "## Changelog", and publishes the draft release.
# Usage: release-highlights.sh [--dry-run] <tag>
# --dry-run prints the new body and does not edit or publish the release.
# Needs gh with write access to autobrr/qui, curl, jq, and the tags in the
# local clone. Needs RELEASE_NOTES_ANTHROPIC_API_KEY.
#
# If the sections cannot be made, the script prints a ::warning:: line and
# publishes the release with the body unchanged. Only a failed publish exits
# non-zero, because a release that stays a draft must fail the job.
set -euo pipefail

dry_run=false
if [[ ${1:-} == --dry-run ]]; then
	dry_run=true
	shift
fi
tag=$1
repo=autobrr/qui
skills_repo=s0up4200/skills
skill_dirs=(skills/aminblg/simple-english skills/cursor/unslop)

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Warnings go to stderr, so that a dry run prints only the body on stdout.
warn() { echo "::warning::release highlights: $*" >&2; }

gh release view "$tag" -R "$repo" --json body,isPrerelease >"$work/release.json"
# The x keeps the trailing newlines, which command substitution drops.
body=$(jq -j .body "$work/release.json"; printf x)
body=${body%x}
prerelease=$(jq -r .isPrerelease "$work/release.json")

# Prints the skill files and the PR data that go into the prompt after the
# instructions.
collect_input() {
	local tree dir path n prev_tag subjects subject json
	tree=$(gh api "repos/$skills_repo/git/trees/main?recursive=1") || {
		warn "cannot list ${skill_dirs[*]} in $skills_repo"
		return 1
	}
	for dir in "${skill_dirs[@]}"; do
		n=0
		while IFS= read -r path; do
			printf '\n### %s\n\n' "$path"
			gh api -H 'Accept: application/vnd.github.raw' "repos/$skills_repo/contents/$path?ref=main" || {
				warn "cannot read $path in $skills_repo"
				return 1
			}
			n=$((n + 1))
		done < <(jq -r --arg dir "$dir/" '.tree[] | select(.type == "blob" and (.path | startswith($dir))) | .path' <<<"$tree")
		if ((n == 0)); then
			warn "skill folder $dir is missing or empty in $skills_repo"
			return 1
		fi
	done

	prev_tag=$(git describe --tags --abbrev=0 "$tag^") || {
		warn "cannot find the tag before $tag"
		return 1
	}
	subjects=$(gh api --paginate "repos/$repo/compare/$prev_tag...$tag" --jq '.commits[].commit.message | split("\n")[0]') || {
		warn "cannot compare $prev_tag...$tag"
		return 1
	}
	printf '\n## Pull requests in %s (since %s)\n' "$tag" "$prev_tag"
	while IFS= read -r subject; do
		# Merge commits and other commits without a PR number add nothing.
		[[ $subject =~ \(#([0-9]+)\)$ ]] || continue
		json=$(gh pr view "${BASH_REMATCH[1]}" -R "$repo" --json number,title,labels,body) || {
			warn "cannot read PR #${BASH_REMATCH[1]}"
			return 1
		}
		# Dependency, CI-only, and docs-only PRs give only their title.
		if [[ $subject =~ ^(chore\(deps\)|ci|docs)[:\(!] ]] ||
			jq -e 'any(.labels[]; .name == "dependencies")' <<<"$json" >/dev/null; then
			json=$(jq 'del(.body)' <<<"$json")
		fi
		jq -r --arg subject "$subject" '
			"\n### PR #\(.number): \(.title)\n\nCommit subject: \($subject)\nLabels: \([.labels[].name] | join(", "))\n"
			+ (if .body then "\n\(.body)\n" else "" end)
		' <<<"$json" || return 1
	done <<<"$subjects"
}

# Prints the Breaking changes and Highlights sections as Markdown.
write_sections() {
	if [[ -z ${RELEASE_NOTES_ANTHROPIC_API_KEY:-} ]]; then
		warn "RELEASE_NOTES_ANTHROPIC_API_KEY is not set"
		return 1
	fi
	{
		cat <<'EOF'
You write two sections of the GitHub release notes for qui, a web UI that self-hosters use to manage qBittorrent instances. The input after these instructions has two writing skills and the pull requests (PRs) in the release. Write only Markdown, with no text before or after the sections.

## Breaking changes

Write this section only when a PR has the "BREAKING CHANGE" label, has a "!" before the colon in its commit subject, or its body says that users must act. If nothing breaks, leave the section out. Never write "None".

- Write one bullet for each breaking change. Do not group them.
- Start with the change in bold, then say in one or two sentences what the user must do. Keep only what the user needs to act: the option name, the new value, the error text that they will see.

## Highlights

- Group by theme, never by PR. A theme can bundle several related PRs into one bullet.
- Order by impact. The first bullet is the change that most users will feel.
- Start each bullet with a bold phrase that ends in a period, then one sentence, two at most.
- Say the outcome: what users can do now, or what stopped breaking. Leave the mechanism, the old behaviour, and edge cases to the commit list below the sections.
- Keep one fact for each bullet: a version requirement, an option name as it shows in the app, or a striking number.
- Use 3 to 6 bullets on a normal release, fewer on a small release, in about 1,000 to 1,500 characters. The last bullet is always "**Fixes and polish.**" with the smaller items, each named in a few words.
- If no change in the release is visible to users, write no Highlights section.
- Use only facts from the PR titles, labels, and bodies. Do not invent features or numbers.
- Do not quote torrent or release names from the PRs.

## Format

Use exactly these headings, Breaking changes first:

```markdown
## Breaking changes
- **What changed.** Who it affects and what they must do.

## Highlights
- **Theme lead-in.** One sentence on what users get.
- **Fixes and polish.** The smaller items, named and comma-chained.
```

Write no title, no contributor or "Thanks to" line, no emoji, and no changelog link. Never use em dashes.

Use this example for the voice and the length (qui v1.21.0):

```markdown
## Highlights
- **Real-time updates that hold up under load.** Updates now arrive as small deltas (~26x less data), so big instances and background tabs stop stalling or going black.
- **Per-file download priority.** Set Normal, High, Maximum, or Do not download per file and folder in the Content tab.
- **Three more languages.** Italian, Korean, and Ukrainian, seven in total.
- **Smarter automations.** New tracker **Status** and **Message** conditions (needs qBittorrent 5.1+), and a **Year** condition parsed from the torrent name.
- **Cross-seed matching fixes.** TV searches return results on BTN and IPT again, and rootless single-file matches land in the right folder.
- **Fixes and polish.** Postgres hardening, a `GET /api/version` endpoint, all-instances exports to the right instance, and UI polish.
```

## Prose passes

After you write the draft, apply the Document rules of the simple-english skill, then apply the unslop skill last. The passes change the sentences, not the headings, the bold lead-ins, or the length. Keep option names, version numbers, and other facts exact. The users know their app, so its terms need no definition.

# Skills
EOF
		collect_input
	} >"$work/prompt.md" || return 1

	jq -n --rawfile prompt "$work/prompt.md" '{
		model: "claude-opus-5-5",
		max_tokens: 16000,
		messages: [{role: "user", content: $prompt}]
	}' >"$work/request.json" || return 1

	local status
	status=$(curl -sS --max-time 600 -o "$work/response.json" -w '%{http_code}' \
		https://api.anthropic.com/v1/messages \
		-H "x-api-key: $RELEASE_NOTES_ANTHROPIC_API_KEY" \
		-H 'anthropic-version: 2023-06-01' \
		-H 'content-type: application/json' \
		--data-binary "@$work/request.json") || {
		warn "the Claude request failed"
		return 1
	}
	if [[ $status != 2* ]]; then
		warn "Claude returned HTTP $status: $(jq -r '.error.message // empty' "$work/response.json" 2>/dev/null)"
		return 1
	fi
	local stop_reason
	stop_reason=$(jq -r .stop_reason "$work/response.json")
	if [[ $stop_reason != end_turn ]]; then
		warn "Claude stopped with stop_reason $stop_reason"
		return 1
	fi
	# Opus 5.5 always returns a thinking block first. Read only the text blocks.
	jq -j '[.content[] | select(.type == "text") | .text] | join("")' "$work/response.json"
}

new_body=$body
nl=$'\n'
# GoReleaser writes LF. A body edited on github.com has CRLF, so a dry run on
# an old release needs the CRLF marker.
for eol in "$nl" $'\r\n'; do
	marker="$nl## Changelog$eol"
	[[ $nl$body == *"$marker"* ]] && break
done
if [[ $nl$body != *"$marker"* ]]; then
	warn "the release body has no '## Changelog' line"
elif ! sections=$(write_sections); then
	: # write_sections printed the warning.
elif [[ -z ${sections//[[:space:]]/} ]]; then
	warn "Claude returned no text"
# A contributor can edit a PR body after the merge. Accept only the two
# headings and bullets, with no links, autolinks, email addresses, or HTML,
# so that text in a PR body cannot put a link into the release.
elif grep -qvE '^(## (Breaking changes|Highlights)|- \*\*.+|[[:space:]]*)$' <<<"$sections" ||
	grep -qiE 'https?://|www\.|\]\(|</?[a-z]+[[:space:]>/]|<[a-z][a-z0-9.+-]*:|[[:alnum:]._+-]@[[:alnum:]-]+\.[a-z]' <<<"$sections"; then
	warn "Claude returned text that is not only the two sections"
else
	b=$nl$body
	new_body="${b%%"$marker"*}$marker${sections//$nl/$eol}$eol${b#*"$marker"}"
	new_body=${new_body#"$nl"}
fi

if $dry_run; then
	printf '%s' "$new_body"
	exit 0
fi

if [[ $new_body != "$body" ]]; then
	printf '%s' "$new_body" >"$work/notes.md"
	gh release edit "$tag" -R "$repo" --notes-file "$work/notes.md" >/dev/null ||
		warn "cannot write the new release body"
fi
publish=(--draft=false)
if [[ $prerelease != true ]]; then
	publish+=(--latest)
fi
gh release edit "$tag" -R "$repo" "${publish[@]}"
