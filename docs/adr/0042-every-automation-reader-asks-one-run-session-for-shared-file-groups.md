---
status: accepted
date: 2026-11-08
---

# Every automation reader asks one run session for Shared-file groups

Each automation run, dry run, and preview opens one session for the Shared-file groups. Every reader asks the session for the groups and the cross-seed facts of the torrents that it needs: the Deletion selection, FREE_SPACE delete rules, move, tag and category actions that include cross-seeds, category block lists, and conditions and sorts on cross-seed fields. The session keeps one torrent snapshot, one candidate index, and the file lists that it read. It reads only the torrents that a reader asks for, and the torrents that their groups lead to. Before this decision, every run built the groups of the whole library and fetched almost every file list from qBittorrent, also when no torrent matched (#3895).

## Considered options

- **A longer file cache age for the group build.** Rejected: after a rename in qBittorrent outside qui, the grouping reads old file names, and a delete can remove files that a torrent still uses.
- **The delete-dialog matcher for the expansion.** Rejected: one unreadable file list fails the whole call, so a rule cancels all its deletes. Its link rules are also different.
- **A scoped build for the run, and the full build for the preview.** Rejected: the run and the preview then read groups in two ways, the drift that ADR 0027 removed.
- **A scoped build for deletes, and the full build for the other readers.** Rejected: two paths for one set of groups drift apart.

## Consequences

- A group is the whole component, with links followed in both directions. When a file list cannot be read, a group can be larger than before, and the run blocks or keeps more.
- When a run deletes in "include" or "preserve" mode on an instance with trusted identity, the check for files shared with another instance covers the whole library. A scoped check can miss a Shared file.
- A score sort on a cross-seed field, and a broad tag action that includes cross-seeds, still read the whole library through the session.
