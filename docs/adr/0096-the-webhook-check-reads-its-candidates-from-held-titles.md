---
status: accepted
date: 2026-11-27
---

# The webhook check reads its candidates from Held titles

autobrr calls the webhook check for each announce that passes a filter. The check read every torrent of every instance, read the database, asked Sonarr and Radarr about the announce name, and could wait up to 60 s for file lists. On a library of 6,400 torrents a "no match" took 1 to 5 s, so users narrowed their filters with Sonarr and Radarr title lists. In 16 hours of production traffic, 135 of 155 checks behind those lists found no match, and a torrent that the lists did not name never reached qui.

The check now looks the announce up in Held titles first, and answers "no match" when nothing is held. Held titles maps each Title key and each exact total size to the torrents that hold it. It uses the title function of the matcher, so it never rejects a pair that the matcher would accept. It lives in memory and is rebuilt from the Sync snapshot at each start, on each instance reconnect, and when the category mapping rules change. Until it is ready, the check answers "no match" at once; RSS finds a release missed in that time later. The full scan is gone from the app. It stays in test code as the reference of the equivalence tests. When a title is held, the Match verdict runs once per Copy group of the torrents that hold it. Helper torrents are never candidates (ADR 0092).

Alternate titles come from an Alternate-title source in the background, once per held title, and are kept in the arr ID cache. This changes ADR 0002 for alternate titles: they are now read for held releases, not for the announce name. The episode map stays per announce name, and is asked for only when the announce or a candidate uses absolute numbering. Sonarr and Radarr are the source today. Librrary is added later as another source, with no change to Held titles or the check.

A torrent added through the client proxy schedules a sync 200 ms later, never sooner than 2 s after the last sync. A check that finds nothing while a sync runs or is due waits for it, at most about 1 s, and starts no sync of its own. Syncs stay at most one per 2 s per instance, however many announces arrive.

The season pack endpoints (ADR 0094) and the automatic season pack assembly of RSS and the Library Scan are the exception: while Held titles is not ready, they keep their fallback to the full torrent list.

## Considered options

- **Fill the autobrr filter with qui's titles through a plain-text list.** Rejected: a new title reaches the filter seconds after the grab, so an announce one second later is blocked, and 5 of 6 production cross-seeds in 16 hours were such races. At 50k torrents, autobrr's title matching costs up to 60 ms and 17 MB per announce, per filter.
- **Store Held titles in a table.** Rejected: a full feed writes a row on every add and remove, qui still has to compare the table against qBittorrent after a restart, and a stale row is a missed cross-seed that survives restarts. The build takes about 1 s at 50k torrents.
- **Ask Sonarr and Radarr on a miss.** Rejected: that puts up to 5 s of network on every announce for a title that qui does not hold.
- **Title keys only, with no alternate titles.** Rejected: anime and foreign-title announces would lose matches.
- **Keep the full scan while Held titles builds.** Rejected: it would be code that runs only for a few seconds after a start, and so it would rot untested, and each check in that time would cost a full scan and a Sonarr call.

## Consequences

- A `forbidigo` rule blocks a read of an instance's full torrent list in the check files. Its message names Held titles.
- A new Alternate-title source passes the shared contract test and adds one line to the startup wiring.
- A torrent whose name has no title stays a candidate until its files are read.
- Users without an Alternate-title source match on titles alone, as before.
- An autobrr filter needs no title list in front of the check.
