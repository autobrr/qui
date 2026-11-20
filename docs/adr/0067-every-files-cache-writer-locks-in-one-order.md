---
status: accepted
date: 2026-11-20
---

# Every files cache writer locks in one order

The files cache has four writers: the upsert after a file list read, the invalidation, the orphan cleanup, and the delete of an instance. On Postgres, two of them that lock the same rows in different orders deadlock (SQLSTATE 40P01).

Each writer runs in one transaction and takes its locks in one order: the instance row, then the sync rows in the order of their `torrent_hash_id`, the string pool ID of the hash, then the file rows. The sync rows come before the file rows so that the second writer of a hash waits for the first to commit before it compares its files with the stored ones. A reader thus never sees the print of one write with the file list of another.

SQLite has one writer at a time, so the order matters only on Postgres. Both databases run the same code.

## Consequences

- A new writer of the files cache takes the same locks in the same order, starting with `lockInstances`.
