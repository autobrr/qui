---
status: accepted
date: 2026-10-25
---

# The live run and the dry run read one Run plan

One plan step in the automations package builds the Run plan. The live run carries it out, and the dry run records it. Before this decision, the run built loose maps that the two forms read in their own ways. The dry run also had its own copy of the group and cross-seed expansion for move and category. Issue #3479.

The plan step only reads. It reads file lists for the overlap check and asks the target instance of an export if it already has the torrent. Both forms need these answers, so the plan holds them. Each write stays in the live run: the managed tag reset, the export in-flight slot, the hardlink recheck, the stamps, the FREE_SPACE cooldown and the batch split.

Where the live run and the dry run disagreed, the live run's behavior became the behavior of the plan, as in ADR 0027.

The category and tag previews build the Run plan of the one rule that the user edits and show their part of it.

## Consequences

- Code that decides outside the plan step which torrents an Action reaches reverses this decision and needs a new ADR.
- A new Action adds one field to the plan.
