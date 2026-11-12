---
status: accepted
date: 2026-11-07
---

# A Copy group narrows the Batch matcher and an unreadable torrent stays ungrouped

The torrent list can collapse the copies of a release. The groups come from the Batch matcher, as ADR 0032 asks of a cross-seed list. Two things differ from the other readers of the matcher. A link needs equal file size lists in every tier, and a torrent with an unreadable file list stays ungrouped. It does not fail the list.

Nothing destructive reads a Copy group. An automation never reads one, because its delete conditions and modes need the links and the unknown state that a Copy group leaves out. A delete of a collapsed row reaches that torrent, and the delete dialog offers its own strict check, as it does today. Thus a wrong or missing group changes only how the list looks. One magnet in 6,000 rows must not remove grouping from the other rows.

Equal size lists also stop a chain. Without them, a season pack links to its episodes, and the episodes then show as copies of each other. With them, the group needs no link check, so grouping makes no disk or SSH calls.

## Considered options

- **Same folder only.** Rejected: 82 % of the tagged cross-seeds in a real library of 6,167 torrents stayed alone, because linked copies have their own folders.
- **Same name and size outside the matcher.** Rejected: it adds a second rule for what a copy is, and it misses retitled copies.
- **All matches as the Cross-Seed tab sees them.** Rejected: chains join rows that are not copies, and a strict list fails on one unreadable torrent.

## Consequences

- A linked copy with an extra file stays ungrouped.
- A Copy group can differ from the Cross-Seed tab of its torrents. The tab also lists a pack for its episodes.
- A new rule that finds more cross-seeds goes into the Batch matcher, and Copy groups get it if the file sizes are equal.
- A group id does not show the instance or hash of the Group representative, and a row with no visible copy has no id, so ADR 0040 holds for a key with an Instance limit.
- Narrows ADR 0032 for this one reader. ADR 0032 still holds for the tab, Filter Cross-Seeds, the delete check, and the automations.
