---
status: accepted
date: 2026-11-11
---

# Every reader of an instance's torrents shares one Sync snapshot

go-qbittorrent publishes one Sync snapshot for each sync that it applied, and each reader in qui gets references to the torrents of that snapshot. No reader copies the library, and no reader writes to a torrent that it read. One package in qui owns the sync object of the library, and no other code can hold it.

Before this decision, each read of the sync cache copied the whole library two times. Memory grew with the size of the library times the number of requests in flight. On a local rig with 20,000 torrents, 32 cross-seed webhook checks at the same time took qui from 400 MB to 2.3 GB, and users reported up to 30 GB.

## Considered options

- **A shared copy that qui makes after each sync.** Rejected: each sync still copies the whole library two times, as one reader does today.
- **A limit on the number of heavy requests at the same time.** Rejected: at 300,000 torrents, 4 requests still cost about 3 GB, and the other requests wait.
- **A torrent type with getter methods only.** Rejected: about 70 methods, each reader changes, and the interface is as large as the data.
- **A lint rule on the copying reads only.** Rejected: the lint does not see a call through an interface. Two such calls existed.

## Consequences

- A reader that needs a changed torrent copies that one torrent. The read that applies the expected state after a user action does this, and so does the routed read of a Cross-seed route, which maps the paths of each torrent that it returns.
- Tracker details are in a lookup by hash, not in the torrent.
- In tests and in builds without a release version, the library seals each snapshot and panics when a reader changed a torrent.
- A memory budget test limits what a list read, a stream tick, and a cross-seed webhook check allocate.
- The lint rejects code outside the owner package that names the sync type of the library or calls its constructors.
- A read in qui that returns copies of all torrents of an instance reverses this decision and needs a new ADR.
