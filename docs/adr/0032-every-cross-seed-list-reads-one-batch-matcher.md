---
status: accepted
date: 2026-10-28
---

# Every cross-seed list reads one batch matcher

The Cross-Seed tab, "Filter Cross-Seeds", the cross-seed check of the delete dialog, and the automations find the cross-seeds of a torrent with one batch matcher. The batch matcher groups the torrents of an instance by name, by Title key and episode, by exact total size, and by shared folder. It compares each torrent only with the torrents of its groups. The tab and the filter ask it with a selection of one torrent, on every active instance.

A rule that finds more cross-seeds goes into the batch matcher, so that every list gets it. A list can only narrow what the batch matcher returns. The delete dialog keeps only the torrents that the delete touches on its instance. The tab keeps every match.

## Considered options

- **Local matches for the tab, the batch matcher for the rest.** Rejected: the tab listed retitled linked copies and linked copies that number their episodes in another way, and the delete dialog and the automations did not (#3555).
- **The tab moves to the batch matcher as it was.** Rejected: the tab stops showing copies that it showed before.

## Consequences

- The batch matcher always applies title rescue. The title rescue setting of cross-seed searches does not apply to it.
- A retitled linked copy with an extra file has a different total size, and no list finds it.
- Every list is strict: a file list that cannot be read fails the list, and only trusted identity counts for a link.
- Supersedes ADR 0030.
