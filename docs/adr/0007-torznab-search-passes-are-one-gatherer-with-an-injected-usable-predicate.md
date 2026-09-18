---
status: accepted
date: 2026-09-18
---

# Torznab search passes are one gatherer with an injected usable predicate

A cross-seed search runs a Torznab primary pass and up to four retries: the yearless retry, the title rescue after a tag-sourced ID primary, the alternate-title pass, and the alternate connector-spelling pass. All five live in `searchGatherer.gather` in `internal/services/crossseed/search_gather.go`. The gatherer takes three functions: `search` for one pass, `idCapIndexers` for the ID-capable indexer set, and a `usable` predicate that says whether a result would survive matching. It decides retries with that predicate and never parses or classifies a title itself. It returns the merged results and the covered indexer IDs as separate outputs. Issue #2747.

## Considered options

- **An interface over `jackett.Service`.** Rejected: the fake has to drive the completion callbacks, and two methods serve one caller. A struct with function fields is the whole seam.
- **The gatherer calls the matcher on `Service`.** Rejected: it ties this change to the matcher extraction and lets matching rules grow inside the gatherer. The caller closes the matcher into the predicate once per search.
- **Return the unsatisfied indexer set.** Rejected: nothing after the passes reads it.
- **A per-indexer yearless retry.** Rejected: dropping the year fires on nearly every movie and has low per-indexer value, so the pass stays gated on the whole search (PR #2400).

## Consequences

- A new retry pass is one block inside `gather` and one row in `TestGatherSearchResults`. The test drives the pass sequence with a recording fake and no HTTP server.
- The covered set never includes an indexer that failed or was rate limited, so the per-indexer search history never stamps it as searched (#2217).
- A change that makes the gatherer classify titles, merges the covered set with the usable set, or turns the yearless retry into a per-indexer retry reverses this decision and needs a new ADR.
