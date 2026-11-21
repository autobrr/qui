---
status: accepted
date: 2026-11-21
---

# Every Torznab search for one source runs through the search gatherer

ADR 0007 put the passes of a cross-seed search in one gatherer. This decision extends it to every search for one source: the cross-seed search, the season-pack search of a Library Scan, and the Dir Scan search. Each caller hands the gatherer its own usable predicate: the match classifier, the season-pack admission check, or the size band of the searchee. Before this decision, Dir Scan ran a second retry loop that lacked two passes and gated on the whole search, and the season-pack search ran no retry. The TV year retry (discussion #3550) would have needed a second copy. Issue #4350.

Retries are per indexer in all three, except the yearless retry, which stays gated on the whole search (ADR 0007). This costs more requests in Dir Scan than its old whole-search gate, and finds the release on trackers that the first tracker's hit used to hide. Every retry still asks the Request slot of its indexer (ADR 0046): a Native indexer waits its Request gap, and an indexer in a Rate-limit cooldown gets no retry. An indexer behind Prowlarr or Jackett has no Request gap, so its retries go out back to back, as cross-seed retries already did.

## Considered options

- **Keep the Dir Scan loop and add the year retry to both.** Rejected: the loops had already drifted, and each new pass would be built twice.
- **Move the gatherer to its own package.** Rejected for now: Dir Scan already imports the cross-seed package, and the alternate-connector query that the gatherer uses lives there.

## Consequences

- A new retry pass is one block in `Gather` and reaches all three searches.
- A `forbidigo` rule in `.golangci.yml` reports a Torznab search for one source sent past the gatherer. The manual search handlers stay outside it: a person runs them and reads the raw results.
- Code that sends such a search without the gatherer reverses this decision and needs a new ADR.
