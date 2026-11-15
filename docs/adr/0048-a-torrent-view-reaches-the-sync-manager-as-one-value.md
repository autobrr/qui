---
status: accepted
date: 2026-11-09
---

# A torrent view reaches the sync manager as one value

A caller that reads the torrent list gives the sync manager one `TorrentListView` value and makes one call. The value holds the instance or the Unified view, the page, the sort, the search, the filters, rows yes or no, Sidebar counts yes or no, Copy groups, freshness, and the passkey access of the reader. No list read takes an option from the request context. Issue #3957.

Before this decision, a caller set up to five context flags, picked one of two methods with eight positional arguments, and applied two passkey steps itself. #3851 removed passkeys from the rows. One day later, #3856 added a second step at five places, because a filter expression was still able to test a passkey.

The qbittorrent package knows the passkey access of the reader, although that fact comes from an API key permission (ADR 0040). The package runs the filter expression and builds the rows, and a passkey can leave only at those two places. A view that says nothing about access hides passkeys, so a caller that forgets the fact shows tracker origins only.

## Considered options

- **Move only the passkey rule into the package.** Rejected: the flags and the two methods stay, and each caller still builds a request from four parts.
- **One read for each purpose: rows, counts, and selection.** Rejected: the Unified choice and the passkey rule repeat in each read.
- **Two values: the view, and a second value for freshness and Sidebar counts.** Rejected: most callers set both facts one time, and the stream leaves the two facts out of its group key.
- **Passkeys shown unless a caller hides them.** Rejected: a caller that forgets the fact leaks a passkey.

## Consequences

- The value rejects a Unified view without rows, because the Unified total counts rows. It also rejects a view without rows that asks for Copy groups.
- The proxy states full passkey access, because its clients need the announce URL.
- The `syncProvider` interface of the stream has four methods (ADR 0005).
- `selectAllTorrents` stays the one place that resolves a selection (ADR 0009). It builds the view.
- ADR 0014 names the two former list methods. `ListTorrents` now serves both views with `torrentOrder`.
- A list option that travels in the request context, or a passkey step in a caller, reverses this decision and needs a new ADR.
