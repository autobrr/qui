# qui

qui manages torrent-client state and workflows for a self-hosted installation.

## Language

- **External Program**: A user-configured executable that qui may invoke with data about one torrent. _Avoid_: Hook, script.
- **Execution Request**: A request to run an External Program for one torrent. _Avoid_: Job, task.
- **Admitted Execution**: An Execution Request accepted for future execution. It does not mean the program started. _Avoid_: Successful execution, started execution.
- **Running Execution**: An Admitted Execution whose External Program has started and has not exited. _Avoid_: Queued execution.

## Cross-seed search

- **Usable result**: A search hit that survives release and size filtering. Retry passes gate on usable results, never on raw hit counts. _Avoid_: Hit, raw result (when gating is meant).
- **Mixed mode**: One search fan-out where ID-capable indexers receive an ID-only query and the rest receive the title query. Lives in the jackett layer; callers opt in with the `OmitQueryForIDs` flag on an ID-carrying movie or TV request. _Avoid_: Hybrid search, dual query.
- **Arr IDs**: External IDs (imdb/tvdb/tmdb/tvmaze) supplied by a Sonarr/Radarr lookup or its cache. The highest-trust ID source.
- **Tag-sourced IDs**: External IDs read from the Matroska tags of the source file. Trusted blind for querying; a wrong tag is caught by result filtering, not by pre-validation. _Avoid_: MediaInfo IDs (ambiguous with the library name), embedded IDs.
- **Per-indexer retry**: A retry pass that re-queries only the indexers holding no usable result, leaving satisfied indexers untouched. Cross-seed success is per tracker, so one indexer's match never blocks another's retry. The yearless retry is a whole-search retry, not a per-indexer one. _Avoid_: Fallback search, retry-all, rescue pass.
- **Title rescue**: A matching rule, not a search pass. A candidate whose title differs from the source is still accepted when the reported total size is exactly equal and every other release attribute matches. Off by default; Skip recheck disables it. _Avoid_: Rescue pass, fuzzy match.
- **Query degradation**: A per-search flag telling the frontend the search ran at lower precision than intended (title-only when IDs were wanted). An ID-quality primary, arr- or tag-sourced, is not degraded.
- **Manual match**: A cross-seed apply where the user chooses the target torrent. Candidate discovery and the category and content-type gates are bypassed; the recheck is the arbiter of a wrong pick. _Avoid_: forced match, pinned match.
- **Numbering scheme**: How a TV release names its episode: seasoned (`S04E15`) or absolute (`- 81`, no season). A pair of releases that use the same scheme compare episode numbers directly. _Avoid_: anime numbering, episode format.
- **Episode map**: The Sonarr-sourced triple (season, episode, absolute) for one release name. It lets one seasoned and one absolute release count as the same episode. Exists only when Sonarr names exactly one episode and that episode has an absolute number; otherwise there is no map and the pair falls back to size evidence. _Avoid_: Sonarr mapping, episode translation, absolute lookup.

## Disc reports

- **Disc**: A Blu-ray as one unit: the folder that holds `BDMV`, or one `.iso`. The unit a BDInfo scan reads. One torrent can hold several Discs. _Avoid_: Blu-ray folder, disc torrent.
- **Disc scan**: One queued or running BDInfo job on one Disc. _Avoid_: BDInfo job, bdinfo run.
- **Disc report**: The cached BDInfo text for one Disc on one instance. _Avoid_: disc info, BDInfo output.
- **Search candidate**: The unit of work in a seeded search run: a source torrent, or a season group formed by season pack automation. A run counts candidates, not torrents. _Avoid_: Torrent (when the count is meant), item.
- **Cross-seed added**: One successful apply into the client. One Search candidate can produce several. _Avoid_: Match, torrent added.
- **Due candidate**: A Search candidate that still needs a search. _Avoid_: Total torrents, pending, remaining.
