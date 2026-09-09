# Issue tracker: GitHub Discussions, then Issues

Bug reports and feature requests arrive as GitHub Discussions in the categories Issue Triage and Feature Requests, Ideas. Discussions are the triage surface. Issues hold only work that an agent or a human can start: a discussion that reaches `ready-for-agent` gets a linked issue. Specs and tickets from `/to-spec`, `/to-tickets`, and `/wayfinder` are issues.

Use the `gh` CLI for all operations. GitHub shares one number space across discussions and issues. Resolve a bare `#42` with `gh discussion view 42` first, then `gh issue view 42`.

## Discussions

- **List**: `gh discussion list --category issue-triage --label needs-triage --state open --json number,title,labels`. Category slugs: `issue-triage`, `feature-requests-ideas`.
- **Read**: `gh discussion view <number> --comments`.
- **Comment**: `gh discussion comment <number> --body "..."`.
- **Labels**: `gh discussion edit <number> --add-label "..." --remove-label "..."`.
- **Close**: `gh discussion` has no close command. Use GraphQL with the discussion node id:

  ```bash
  ID=$(gh api graphql -f query='query($n:Int!){repository(owner:"autobrr",name:"qui"){discussion(number:$n){id}}}' -F n=<number> --jq .data.repository.discussion.id)
  gh api graphql -f query='mutation($id:ID!,$r:DiscussionCloseReason!){closeDiscussion(input:{discussionId:$id,reason:$r}){discussion{url}}}' -f id="$ID" -f r=RESOLVED
  ```

  Reasons: `RESOLVED`, `OUTDATED`, `DUPLICATE`.
- **Promote to issue** (`bug` only, see `docs/agents/triage.md`): `gh issue create --title "..." --label ready-for-agent --body "..."` with `From discussion #<number>` as the last line of the body. Then comment the issue URL on the discussion and close the discussion with reason `RESOLVED`.

In the triage workflow, `./.github/scripts/discussion-write.sh` wraps these writes and binds them to the discussion under triage. The playbook in `docs/agents/triage.md` is the process.

## Issues

- **Create**: `gh issue create --title "..." --body "..."`. Use a heredoc for a multi-line body.
- **Read**: `gh issue view <number> --comments`.
- **List**: `gh issue list --state open --label ready-for-agent --json number,title,body,labels`.
- **Comment**: `gh issue comment <number> --body "..."`.
- **Labels**: `gh issue edit <number> --add-label "..."` / `--remove-label "..."`.
- **Close**: `gh issue close <number> --comment "..."`.

## Pull requests as a triage surface

**PRs as a request surface: no.** External PRs are reviewed by a human, not triaged.

## When a skill says "publish to the issue tracker"

Create a GitHub issue.

## When a skill says "fetch the relevant ticket"

Run `gh issue view <number> --comments`. If the number is a discussion, run `gh discussion view <number> --comments`.

## Wayfinding operations

Used by `/wayfinder`. The **map** is a single issue with **child** issues as tickets.

- **Map**: a single issue labelled `wayfinder:map`, holding the Notes / Decisions-so-far / Fog body. `gh issue create --label wayfinder:map`.
- **Child ticket**: an issue linked to the map as a GitHub sub-issue (`gh api` on the sub-issues endpoint). Where sub-issues are not enabled, add the child to a task list in the map body and put `Part of #<map>` at the top of the child body. Labels: `wayfinder:<type>` (`research`/`prototype`/`grilling`/`task`). Once claimed, the ticket is assigned to the driving dev.
- **Blocking**: GitHub native issue dependencies. Add an edge with `gh api --method POST repos/autobrr/qui/issues/<child>/dependencies/blocked_by -F issue_id=<blocker-db-id>`, where `<blocker-db-id>` is the numeric database id of the blocker (`gh api repos/autobrr/qui/issues/<n> --jq .id`, not the `#number` or `node_id`). GitHub reports `issue_dependencies_summary.blocked_by` (open blockers only). Where dependencies are not available, fall back to a `Blocked by: #<n>, #<n>` line at the top of the child body. A ticket is unblocked when every blocker is closed.
- **Frontier query**: list the open children of the map, drop any with an open blocker or an assignee, first in map order wins.
- **Claim**: `gh issue edit <n> --add-assignee @me`, the first write of the session.
- **Resolve**: `gh issue comment <n> --body "<answer>"`, then `gh issue close <n>`, then append a context pointer to the Decisions-so-far of the map.
