---
status: accepted
date: 2026-11-20
---

# A link tree rollback removes only what its create made

Two torrents can share a link destination. A cleanup that removed every path of a plan after a failed add deleted the links of a sibling torrent that had succeeded (discussion #2282).

Each create of a hardlink tree or a reflink tree returns a record of the files and folders that this call made on disk. Rollback and `RemoveTree` remove the paths in that record and nothing else. A target that already exists is skipped only when the backend can prove that it is the same file as its source, and it does not enter the record, because it belongs to the add that made it first. Every other existing target refuses the whole plan. A hardlink tree refuses with `hardlinktree.ErrTargetExists`. A reflink tree cannot prove identity, so it refuses each existing target with an error that does not wrap that sentinel.

## Consequences

- A plan root that existed before the create stays after a rollback. The caller prunes it.
- A caller that loses the record cannot remove the tree through the plan. It must keep the record until the add is settled.
- `TestHardlinkTree_ExecTierSkippedLinkSurvivesLaterFailure` holds the rule for the remote backend.

## Considered options

- **Remove every path of the plan.** Rejected: it deletes links that another torrent seeds from.
