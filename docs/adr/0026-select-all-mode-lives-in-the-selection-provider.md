---
status: accepted
date: 2026-10-25
---

# Select all mode lives in the selection provider and changes only through user events and scope changes

One reducer in `TorrentSelectionProvider` holds the torrent list selection: the ticks, Select all mode, and the Exclusions. The desktop table and the mobile cards send events to it, and the header reads it. Before this decision, each layout kept its own copy and the provider kept a third, so a fix landed in one layout only. Issue #3480.

The desktop header checkbox and the mobile "Select all" button pick their event by different rules. The header checkbox clears when anything is selected, and selects all otherwise. The mobile button clears when everything is selected, and selects all otherwise. The mobile button shows only when something is selected, so the header rule would make it a clear button.

The reducer never removes a tick or an Exclusion for a torrent that is not in the loaded rows. The loaded rows cannot tell a deleted torrent from a torrent on a page that is not loaded. If the reducer removed a stale Exclusion, that torrent would become selected again, and a bulk delete could remove it. A stale entry does no harm: qBittorrent skips an unknown hash, and a select-all request drops the Exclusions that do not match its scope. The selection count can be off by the stale entries. The count is display only. A select-all export reads every matching page and does not stop at the count.

A change of instance, of the instance subset in the Unified view, of filters, of column filters, of search, or of layout clears the selection. A sort change does not. The Torrents page sends the scope, and it clears the selection when the user leaves the page. The page renders a separate list for each layout, so a layout cannot own this. The selection clears when the viewport crosses 768 px, because each layout builds a select-all request from a different query. The mobile cards ignore column filters, and their search box filters without the route. Otherwise, a bulk delete from the other layout reaches torrents that the user did not select. The mobile cards also clear the selection when their own search changes. A sort, or a live value such as speed in the sort key, can move a ticked torrent past the loaded rows. Thus each layout keeps the last row that it saw for each tick. An action that needs the row, such as an export, still reaches that torrent. A request builds its Exclusions and the mobile targets from the selection keys, not from the loaded rows.

The desktop table does not use the row selection of TanStack Table. Each checkbox reads the reducer.

## Consequences

- A new selection control sends an event to the provider. It does not keep its own selection state.
- A call to `toggleSelected`, `getIsSelected`, or `toggleAllPageRowsSelected` fails the type check, because `torrentTableFeatures` does not include `rowSelectionFeature`.
- ADR 0010 does not change: `selectAllFilters` stays in the desktop table.
