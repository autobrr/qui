---
status: accepted
date: 2026-09-20
---

# A torrent filter reaches the wire through one serializer

The backend `FilterOptions` has no `expandedCategories` field. The sidebar computes `expandedCategories` and `expandedExcludeCategories` for subcategory requests and keeps `categories` as the user's selection, so a request must fold the expanded list into `categories` before it goes on the wire. `web/src/lib/api.ts` does that fold in one private `serializeFilters` function, called at every api method that puts `filters` on the wire, and drops the expanded fields from the payload. No hook or component folds.

A select-all payload carries the visible set: the column filters AND the filter expression, joined by `combineFilterExpr` in `web/src/lib/torrent-filters.ts`. Bulk actions (#1925), field copy, and export all send the same scope. The list request is the one deliberate difference: in cross-seed mode it sends the hash expression alone, because column filters apply client-side then. Issue #2750.

## Considered options

- **A `useTorrentFilterScope` hook that the table, selection, details panel, and cross-seed consume.** Rejected: no consumer needs the list expression, the select-all expression, the search, and the cross-seed detection at once, and the fold is a wire rule and not a hook concern.
- **A helper each caller invokes before it calls the api.** Rejected: a sixth caller forgets it. Export-all did, and exported the parent category without its children.

## Consequences

- A new api method that takes `filters` calls `serializeFilters` and joins the table test in `lib/__tests__/api.torrents.test.ts`.
- A new select-all consumer uses `selectAllFilters` from `useTorrentSelectionDerivations`.
- A fold of `expandedCategories` anywhere outside `api.ts` reverses this decision and needs a new ADR.
