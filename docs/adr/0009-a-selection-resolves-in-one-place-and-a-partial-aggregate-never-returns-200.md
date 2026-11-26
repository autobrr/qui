---
status: accepted
date: 2026-09-20
---

# A selection resolves in one place and a partial aggregate never returns 200

The torrents handler turns a request into torrents through two methods in `internal/api/handlers/torrents.go`: `explicitTargets`, the pure parse of `targets` and `hashes` into hashes per instance, and `selectAllTorrents`, which reads every torrent in scope and drops the excluded ones. Both return the torrents they resolve, `selectAllTorrents` as `CrossInstanceTorrentView` values, so a copy request reads each instance once and a bulk action flattens the views to hashes. A read that could not reach every scoped instance returns `errPartialResults`, and each handler maps it to a 503, because a truncated value list or a half-applied action behind a 200 reads as complete. The sync manager holds no copy of these rules. Issue #2749.

## Considered options

- **A resolver that returns hashes by instance.** Rejected: the copy path reads field values off the views, so a targets-only resolver would make it read each instance twice.
- **An interface over the three read methods.** Rejected: no second adapter exists, and ADR 0005 allows an interface only when a test fake does.
- **The 400 for bare hashes in the unified scope inside the shared code.** Rejected: it is an action rule and not a resolution rule (#2530). `BulkAction` rejects bare hashes because an action on every copy of a hash is not what the user asked for; `GetTorrentField` resolves them across the scope because a field read over duplicate copies is harmless.

## Consequences

- A new selection mode is one branch in `selectAllTorrents`.
- Exclusion matches on the hash, infohash v1, and infohash v2 for copy and for actions alike, and the `hash` field formats the same on the single-instance route and the unified route.
- A handler that reads instances for a selection without going through `explicitTargets` or `selectAllTorrents` reverses this decision and needs a new ADR.
