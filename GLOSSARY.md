# qui

qui manages torrent-client state and workflows for a self-hosted installation.

## Language

- **External Program**: A user-configured executable that qui may invoke with data about one torrent. _Avoid_: Hook, script.
- **Execution Request**: A request to run an External Program for one torrent. _Avoid_: Job, task.
- **Admitted Execution**: An Execution Request accepted for future execution. It does not mean the program started. _Avoid_: Successful execution, started execution.
- **Running Execution**: An Admitted Execution whose External Program has started and has not exited. _Avoid_: Queued execution.

## Torrent filtering

- **Sidebar filters**: Torrent conditions shown in the filter sidebar, including status, category, tag, tracker, and custom expression filters. _Avoid_: All filters when referring only to sidebar filters.
- **Column filters**: Torrent conditions set through table column controls, such as a ratio below 1. _Avoid_: Column sorting.
- **Torrent search**: A query entered in the torrent search box to select matching torrents. _Avoid_: Global filter.

## Torrent list

- **Unified view**: One torrent list built from every active instance, or from the instances the user picks. A subset of instances is still the Unified view. Its totals add up torrents, so a torrent cross-seeded on two instances counts twice. _Avoid_: Cross-instance view, all-instances view.
- **View parity**: The Unified view, scoped to one instance, shows the same rows, order, row fields, and totals as that instance's own view. With more instances, the rows of each instance keep the order that instance's own view gives them. _Avoid_: Consistency, sync.

## Orphan scan

- **Partial scan**: An orphan scan that completed at least one selected scan path but could not complete every selected scan path. _Avoid_: Clean scan, failed scan.

## Release classification

- **Content type**: The kind of media a release name describes: movie, TV, music, audiobook, book, comic, game, app, adult, or unknown. Automations decide it from the name alone; cross-seed also corrects it with the torrent's file sizes. Magazines are books. Courses are unknown: rls gives video courses and book publishers the same type, so no category filter fits them all. When the caller passes no categories, an indexer search picks them from the query and IDs, which is not a content type. _Avoid_: category (a qBittorrent category is a different thing), media type (the disc format read from a RIAJ code).

## Cross-seed search

- **Usable result**: A search hit that survives release and size filtering. Retry passes gate on usable results, never on raw hit counts. _Avoid_: Hit, raw result (when gating is meant).
- **Mixed mode**: One search fan-out where ID-capable indexers receive an ID-only query and the rest receive the title query. Lives in the jackett layer; callers opt in with the `OmitQueryForIDs` flag on an ID-carrying movie or TV request. _Avoid_: Hybrid search, dual query.
- **Arr IDs**: External IDs (imdb/tvdb/tmdb/tvmaze) supplied by a Sonarr/Radarr lookup or its cache. The highest-trust ID source.
- **Tag-sourced IDs**: External IDs read from the Matroska tags of the source file. Trusted blind for querying; a wrong tag is caught by result filtering, not by pre-validation. _Avoid_: MediaInfo IDs (ambiguous with the library name), embedded IDs.
- **Per-indexer retry**: A retry pass that re-queries only the indexers holding no usable result, leaving satisfied indexers untouched. Cross-seed success is per tracker, so one indexer's match never blocks another's retry. The yearless retry is a whole-search retry, not a per-indexer one. _Avoid_: Fallback search, retry-all, rescue pass.
- **Title rescue**: A matching rule, not a search pass. A candidate whose title differs from the source is still accepted when the reported total size is exactly equal and every other release attribute matches. Off by default; Skip recheck disables it. _Avoid_: Rescue pass, fuzzy match.
- **Query degradation**: A per-search flag telling the frontend the search ran at lower precision than intended (title-only when IDs were wanted). An ID-quality primary, arr- or tag-sourced, is not degraded.
- **Trigger**: How a cross-seed was found: RSS, webhook, seeded search, completion, or interactive apply. Tags, auto-resume, and source filters are set per Trigger. _Avoid_: source (that is the source torrent), request source, mode.
- **Manual match**: A cross-seed apply where the user chooses the target torrent. Candidate discovery and the category and content-type gates are bypassed; the recheck is the arbiter of a wrong pick. _Avoid_: forced match, pinned match.
- **Numbering scheme**: How a TV release names its episode: seasoned (`S04E15`) or absolute (`- 81`, no season). A pair of releases that use the same scheme compare episode numbers directly. _Avoid_: anime numbering, episode format.
- **Episode map**: The Sonarr-sourced triple (season, episode, absolute) for one release name. It lets one seasoned and one absolute release count as the same episode. Exists only when Sonarr names exactly one episode and that episode has an absolute number; otherwise there is no map and the pair falls back to size evidence. _Avoid_: Sonarr mapping, episode translation, absolute lookup.
- **Gazelle-only run**: A library search with Torznab off; candidates come only from the OPS/RED APIs. Reached by the Torznab switch on the Library card, which applies to the next run and is not saved, never inferred from the indexer selection. Needs one Gazelle key. _Avoid_: Torznab-disabled run, forced Gazelle-only.
- **Season numeral**: A Roman numeral at the end of a release title that equals the release's season, as in "Kaiju Squad 100 III" for season 3. Matching reads the title with and without it, and the alternate-title search tries the title without it. _Avoid_: roman suffix, sequel number.

## Cross-seed link tree

- **Linked file**: A file in an added torrent that qui materialized from local data (hardlink or reflink) before the add. _Avoid_: Matched file, existing file.
- **Pending file**: A file in an added torrent that was absent at add time. _Avoid_: Missing file, extra file (when the download is meant).

## Automations

- **Season pack status**: What `SEASON_PACK_STATUS` reports for one torrent: `pack` for a season pack, `packed` for an episode that a season pack of the same release covers, `unpacked` for an episode with no such pack, empty when the name has no season or more than one season. "Same release" means title, season, cut, other markers, language markers, resolution, source, codec, audio, channels, HDR, and group all match. _Avoid_: Packed status, pack coverage.

## Disc reports

- **Disc**: A Blu-ray as one unit: the folder that holds `BDMV`, or one `.iso`. The unit a BDInfo scan reads. One torrent can hold several Discs. _Avoid_: Blu-ray folder, disc torrent.
- **Disc scan**: One queued or running BDInfo job on one Disc. _Avoid_: BDInfo job, bdinfo run.
- **Disc report**: The cached BDInfo text for one Disc on one instance. _Avoid_: disc info, BDInfo output.
- **Search candidate**: The unit of work in a seeded search run: a source torrent, or a season group formed by season pack automation. A run counts candidates, not torrents. _Avoid_: Torrent (when the count is meant), item.
- **Cross-seed added**: One successful apply into the client. One Search candidate can produce several. _Avoid_: Match, torrent added.
- **Due candidate**: A Search candidate that still needs a search. _Avoid_: Total torrents, pending, remaining.

## Instance configuration

- **Preferences**: qBittorrent's own application preferences, which qui reads and writes through the qBittorrent WebAPI. _Avoid_: Settings (when qBittorrent stores the value).
- **Settings**: Configuration that qui stores itself, for example the tracker reannounce and orphan scan configuration. _Avoid_: Preferences (when qui stores the value).

## Allowed Hosts

- **Allowed Hosts**: The optional list of hostnames and IP addresses a request may use to reach qui. Empty means every host. _Avoid_: Host allowlist, host filter.
- **Received Host**: The `Host` header, or the HTTP/2 `:authority`, as the main listener sees it. Never `X-Forwarded-Host`. _Avoid_: Forwarded host, original host.

## Updating qui

- **Self-update**: qui replaces its own binary with a verified release, then restarts. _Avoid_: Upgrade, auto-update.
- **Restart**: qui shuts down the same way it does on SIGTERM and starts again as the same process. _Avoid_: Reload (suggests a config re-read without a restart).
- **Install method**: How the qui binary got onto the host and who manages it: manual install, MSI, seedbox installer, swizzin, container image, or package manager. _Avoid_: Installation type, deployment.
- **MSI**: The Windows package that installs qui for one Windows account, with a Start Menu entry and an uninstall entry. Self-update keeps working after it. _Avoid_: Installer (that is the seedbox installer), setup.exe.
- **App container**: A container that runs qui as its application, such as Docker, Podman, or a Kubernetes pod. The image owns the binary, so an update means a new image. A system container with its own init, such as Proxmox LXC, is not an App container. _Avoid_: Container (when the difference matters), Docker install.

## Running on Windows

- **Tray**: The icon qui shows in the Windows notification area while it runs in a user's logon session. It replaces the console window as the user's handle on a running qui. _Avoid_: Systray, tray app, GUI mode (the GUI is the web UI).

## Logs

- **Log level**: The server setting, from config, env, or Settings, that decides which lines qui writes to stdout and to the log file. The live log view can only receive lines at or above it. _Avoid_: Log filter, verbosity.
- **Level filter**: A user's choice in the live log view of which received levels to show. It is remembered across visits and never changes what qui writes. _Avoid_: Log level (for the viewer choice).
