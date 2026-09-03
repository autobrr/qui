---
status: accepted
date: 2026-09-03
---

# The episode map lives in the arr ID cache

Cross-seed cannot equate an absolute-numbered anime episode (`- 81`) with its seasoned counterpart (`S04E15`) without per-episode data from a metadata provider. Sonarr's parse endpoint, which qui already calls once per search source and once per announce to get external IDs, returns the matched episode with its season, episode, and absolute number. qui reads that episode map from the same response and stores it in the same `arr_id_cache` row, keyed on the release name and expiring with the IDs. The map is read for the name qui already looks up, never for candidates or local torrents, so one lookup serves both directions.

## Considered options

- **Per-series episode table** from the Sonarr episodes endpoint. Exact for every episode of a show, but a series-wide fetch per lookup and a second cache. Kept as the fallback if the parse response proves to miss.
- **Relax the episode number on a within-tolerance size** in the webhook check, with no map at all. Rejected: same-group anime episodes sit within a percent or two of each other, so every announce for a show would report ready against neighbouring episodes and cost a wasted `.torrent` fetch each time.

## Consequences

- A cache row without a map records that it looked, so non-anime lookups do not re-hit Sonarr until the row expires. Rows written before the map existed refetch once.
- The map adds equalities only. When a tracker numbers an episode differently from Sonarr, the pair is an episode mismatch as before and the exact-size tier decides.
- Season packs are out of scope; they need a map per file.
