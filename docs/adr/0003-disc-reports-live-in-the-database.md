---
status: accepted
date: 2026-09-05
---

# Disc reports live in the database

A Disc scan reads a whole Blu-ray at disk speed, so one Disc report can cost twenty minutes and qui must keep it. qui stores the report text in the Disc scan row, keyed on the instance and the resolved on-disk path of the Disc. Seeding folders stay untouched and read-only mounts work.

## Considered options

- **`.bdinfo` file beside the Disc**, as the BDInfo CLI does. Rejected: it writes into content qBittorrent seeds, fails on read-only mounts, and leaves the report behind when the torrent is removed.
- **Key the cache on inode or content hash** so hardlinked copies at two paths share one report. Rejected: two scans of a hardlinked Disc is a rare cost, and inode identity needs per-platform code.
- **Key the cache on path alone**, shared across instances. Rejected: two instances on different hosts can hold different discs at the same path once remote filesystem access exists.

## Consequences

- Discs are treated as immutable. No automatic invalidation; the user rescans.
- Rows are kept when the torrent is deleted. Cleanup is a later problem if anyone hits it.
- The scan runs where the bytes are. Only the one `bdinfo.Run` call knows the disc is local, so remote support (#1917) replaces that call, by exec of a user-installed `bdinfo` or by an SFTP-backed go-bdinfo filesystem, rather than adding a read primitive to `fsops.Backend`. Until it exists, a scan on a remote instance fails with a visible error.
