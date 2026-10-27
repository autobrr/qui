---
status: accepted
date: 2026-10-25
---

# The run, the dry run and the preview take the Deletion selection from one module

`selectDeletions` in `internal/services/automations/deletion_selection.go` builds the Deletion selection for the automation run, the dry run, and the delete preview. Before this decision, the run and the preview each had their own copy of the delete logic, and the two copies called the Shared-file group index, `verifyFileOverlap`, and the free-space projection in different ways. This caused #2775, #3006, #3124 and #2351. Issue #3472.

When the run and the preview disagreed, the behavior of the run became the behavior of the module. There is one exception. The run expanded the Delete triggers in map order, so the activity log gave a shared cross-seed to a different rule from run to run. The module expands the triggers in the sort order of each rule.

The preview passes only the rule that the user edits. If an earlier rule claims a torrent in the run, the preview of a later rule still lists that torrent. A preview over all saved rules is a separate change.

The hardlink recheck (`blockedDeleteCandidates`) stays outside the module, in the live run after the module. The preview and the dry run build the hardlink index in the same second as the selection, so a recheck there finds nothing new.

## Consequences

- Code that selects deletes outside the module reverses this decision and needs a new ADR.
- The other actions of the run skip each claimed Delete trigger and each selected torrent.
