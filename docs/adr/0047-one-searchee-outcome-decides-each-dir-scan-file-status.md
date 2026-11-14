---
status: accepted
date: 2026-11-09
---

# One Searchee outcome decides each Dir Scan file status

One module in the Dir Scan package, the Searchee outcome, decides the status and the searched indexers of each file of a Searchee after a run. The search step, the match step, and the add step report what happened to each Work item. The module returns one decision for each file, and one persist step writes it. Before this decision, each step decided one part of the status and passed it on as flags. The rule "do not downgrade a matched file" existed four times. Issue #3955.

The module ranks the statuses: matched, in qBittorrent, already seeding, error, pending, no match. The order of the events does not change the result. On a tie, the decision of an episode search wins over the decision of its season search.

The module reads an add through the Result class that the result of the Adder states. Dir Scan keeps one case of its own: an add that stays paused because the Settle queue was full is a failed add in Dir Scan.

The module owns only what a search or an add tells about a file. The walk gives a new file its first status and resets a changed file. The run loop chooses which Work items to search.

When the module replaced the old join, no file got a different status. The bugs #2736 and #3664 stayed, as rows in the test table of the module.

## Considered options

- **Fix each status bug in place with one more flag.** Rejected: #3661 added one result and passed it through five functions, and the next fix costs the same.
- **Store one answer for each indexer on each file.** Rejected for now: it needs a migration and UI work. It can build on this module.

## Consequences

- A new status rule is one change in the module and one row in its table test.
- Code outside the module that decides a file status from a search or an add reverses this decision and needs a new ADR.
