---
status: accepted
date: 2026-11-25
---

# Shared torrent reads hide Helper torrents unless a reader asks for them

The sync manager reads that list all torrents of an instance leave out Helper torrents, and so do the finish and add events that it delivers. A reader that needs them asks for them by name: the torrent list with its Sidebar counts, the Dashboard stats, tracker health, the delete guard, folder cleanup, and the file list cache cleanup. A read of named hashes finds them. Before this decision, about 12 features filtered Helper torrents each on their own, and a feature that did not know about them got them. Held titles matched a webhook against a Transfer torrent this way (#4519), and Copy groups grouped a Share torrent with its real torrent.

## Considered options

- **The Sync snapshot holder hides them.** Rejected: a lookup by hash would miss them, and the holder would have to know about Transfers.
- **A wrapper over the sync manager that each feature receives at startup.** Rejected: a feature that gets the sync manager before the wrapper goes on reads raw. The cross-seed Adder and Copy groups job did.
- **Each feature filters, and a test forces each reader to declare a choice.** Rejected: the choice stays in each feature, and the safe answer is not the default.

## Consequences

- The real torrents of a running Transfer stay visible to every read. Only actions on them are blocked.
- The lint forbids the full-list calls of the raw Sync snapshot outside the qBittorrent package.
