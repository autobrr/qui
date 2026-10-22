---
status: accepted
date: 2026-10-20
---

# Tracker health follows the maindata announce flags

On qBittorrent 5.2 and later, qui finds changes in tracker health from three announce flags in `sync/maindata`: `has_tracker_warning`, `has_tracker_error`, and `has_other_announce_error`. Each 60 s pass compares the flags of each torrent with the previous pass. qui fetches tracker details only for a torrent whose flags changed, and for a new torrent with a flag set. qui fetches a flagged torrent again every 10 minutes. The tracker domains and URLs come from the `trackers` map in `sync/maindata`. On qBittorrent 5.1, qui still fetches the tracker details of all torrents every 60 s, because that version does not send the flags.

The previous design fetched the tracker details of the whole library every 60 s. At 50,000 torrents that response was 7.6 MB gzip, and qBittorrent used 100 % CPU for 8 to 14 s. The torrent list stream had gaps during that time.

Each fetch names its hashes, 100 per request. At 50,000 torrents, one request of 100 hashes blocks qBittorrent for about 70 ms, and one request for the whole library blocks it for 8.7 s. Requests of 100 cost more CPU in total when they cover most of the library: 16 CPU-seconds for all 50,000 torrents, against 8.5 for one request. qui still uses them, because a short block keeps the stream without gaps. An automation with a tracker status or tracker message condition needs the details of every torrent. qui keeps a second cache for it with the same rules. The first run fetches every torrent in requests of 100, about 38 s at 50,000 torrents. A later run fetches only the torrents whose flags changed, that qui edited, or whose details are older than 10 minutes.

The flags show that a torrent has a tracker problem, but they do not carry the message. When the message changes and the flags do not, qui shows the new message within 10 minutes, not within 60 s. Each flag is set when any tracker of the torrent has that state. For a torrent with more than one tracker, one tracker can change while another tracker keeps the flags the same. An example is a working tracker that goes down while a second tracker is already down. qui then shows the new health within 10 minutes. A fetch on each pass for these torrents would cost most of the fetch volume that this design removes, because a torrent that lists many trackers, some always down, keeps a flag set.
