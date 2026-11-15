---
status: accepted
date: 2026-11-13
---

# Transfer history stores what each torrent moved

qui records Transfer history from the cumulative upload and download counters of each torrent. It stores the difference between two readings for each torrent and day. A lower reading starts a new count and adds nothing negative. The bytes that a torrent had at the first recording are the Baseline, with no date. A period total is the sum of the torrent rows. It is never the all-time counter of the qBittorrent server, because that counter includes protocol traffic and cannot be split by tracker.

Small totals tables hold the same bytes for each tracker domain and hour, and for each tracker domain, category, and day. Both tables split the bytes by the Statistics cross-seed flag. They fix the category and the flag at the time the bytes moved. Tracker names and merges apply on read, so a rename or a merge changes the past.

## Considered options

- Store sums for each tracker only. qui cannot then name the top torrents, and it cannot group by release group or resolution later.
- Read every view from the torrent rows, so that a category change also moves old bytes. A year at 50,000 torrents takes about 2 seconds in this form.
- Use the Prometheus gauges. They drop when a torrent leaves, and they need two more services.

## Consequences

- The shape of the stored data is hard to change after the first release, because the old bytes cannot be recorded again.
- A torrent that moves to another category keeps its old bytes under the old category. Its Baseline stays under the tracker domain and category of its first recording.
- Each reading copies the values that the recorder needs from each torrent. This is the exception to ADR 0051 for the recorder: a reading waits in a queue, and a reference to the Sync snapshot would keep the whole snapshot in memory. The adapter builds at most one reading each 50 seconds for each instance, one at a time, and at most 4 readings of an instance wait.
- The upload from Statistics cross-seeds in the totals leaves out the bytes that a torrent uploaded before it got the flag.
- The totals of the Statistics page never equal Server Statistics on the Dashboard.
- The hours and the torrent days are kept only for the Detail retention. After it, qui deletes the hourly totals, because the daily totals hold the same bytes, and it folds the daily bytes of each torrent into one row for each UTC year. A query for hours or for the days of a torrent in that time gets these wider rows. The total of a whole UTC day does not change.
