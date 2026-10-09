---
status: accepted
date: 2026-10-03
---

# qui owns the torrent sort

qui sorts the torrent list itself. It does not use the sort of the go-qbittorrent library. `torrentOrder` in `internal/qbittorrent/torrent_sort.go` gives the order of each sort column. `GetTorrentsWithFilters` (the view of one instance) and `GetCrossInstanceTorrentsWithFilters` (the Unified view) both use it, so view parity holds for each column. In the Unified view, two rows that compare equal sort by instance.

Before this decision, the view of one instance used the library sort for most columns and qui comparators for nine columns. The Unified view had its own switch with one case for each column. The two drifted apart in each column: the tiebreaks, the letter case, the status order, and total size. Issue #2989.

A plain column sorts by the value of its `qbt.Torrent` field, then by hash, and descending flips both. This is the library rule. Reflection over the JSON tags finds the fields, so a field that go-qbittorrent adds later sorts without an edit. The columns with their own rule (name, state, tracker, priority, ETA, and the four date columns) keep the order they had in the view of one instance.

For a state sort, the Unified view asks each instance for live tracker health with a request-context option. It does not pass the sort key, because each instance then sorts rows that the merge sorts again.

## Considered options

- **Two comparators and a parity test.** The view of one instance keeps the library sort, and the Unified view keeps its own comparator. A test makes sure that both give the same order. Rejected: each sort change must go into two places, and the test finds the drift only after it happens. With one comparator, parity holds by construction. The deletion test also passes. This test asks what breaks when a module is deleted. Delete `torrentOrder`, and each view loses its sort, so the module carries real behavior.

## Consequences

- A new sort column gets one rule in `torrentOrder`, and both views use it.
- `TestSingleInstanceSortGolden` holds the order of each column. A change to the order updates `testdata/single_instance_sort.golden` in the same pull request.
- `TestUnifiedSortMatchesSingleInstance` fails when a view gets its own sort again.
