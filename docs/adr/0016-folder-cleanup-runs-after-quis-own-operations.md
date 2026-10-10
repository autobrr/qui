---
status: accepted
date: 2026-10-07
---

# Folder cleanup runs after qui's own operations, not after any torrent removal

Folder cleanup starts only from a delete or a move that qui makes or proxies: a delete with files, Set Location, a category change, turning on Automatic Torrent Management, or an edit of a category's save path, from the UI, an automation, or a proxied client. Each of these snapshots the torrents before it sends the request and hands the snapshots over once qBittorrent accepts it. Folder cleanup does not start when a torrent disappears from qBittorrent's torrent list. Issue #2947.

## Considered options

- **Watch the sync data for removed torrents.** It would also catch deletes made in the qBittorrent WebUI. Rejected: go-qbittorrent drops a removed torrent before qui sees the update, and qui keeps no earlier copy of the torrent data. qui would have to keep a second copy of every torrent's paths, and it still could not tell a delete from a move.

## Consequences

- Deletes made in the qBittorrent WebUI, by qBittorrent's share-limit actions, or by a client that talks to qBittorrent directly leave their folders to orphan scan's abandoned directories.
- A new qui operation that deletes or moves content must call `PrepareFolderCleanup` before its request and `Queue` after qBittorrent accepts it, or its folders stay behind.
