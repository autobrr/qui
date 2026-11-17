---
status: superseded by 0032
date: 2026-10-26
---

# The delete check and automations pick candidates with one batch matcher

The cross-seed check of the delete dialog and the automations pick the candidates of a torrent with the same batch matcher. The batch matcher groups the torrents of an instance by name, by Title key and episode, and by shared folder. It compares each torrent only with the torrents of its groups, so one request checks a selection of 50,000 torrents in seconds. Before this change, the delete dialog asked Local matches once for each loaded row, and a select-all checked only the loaded rows (#3341).

A delete check can only narrow what the batch matcher returns. A rule that finds more candidates goes into the batch matcher, so that automations get it too. A candidate rule that only the delete check has gives two answers for one library.

## Considered options

- **Local matches for each selected torrent.** Rejected: 300 rows of a 50,000-torrent library took 91 s.
- **A delete-only rule for a linked copy that numbers its episodes in another way.** Rejected: the delete check then finds copies that automations do not find.

## Consequences

- The delete check does not apply title rescue, and it does not find a linked copy that numbers its episodes in another way. Before this change, the delete dialog found both. The hardlink mode docs state this.
- Only a content path, hardlink, or reflink match becomes a delete match. Only trusted identity counts for a link.
