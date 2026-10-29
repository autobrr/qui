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
- **Select all mode**: A selection of every torrent that matches the current filter, including torrents on pages that are not loaded, minus the torrents that the user excluded. _Avoid_: Select-all, all selected.
- **Exclusion**: A torrent that the user unticked in Select all mode. It stays out of the selection until the user ticks it again or the selection clears. _Avoid_: Deselected torrent.
- **Share limits**: The ratio limit, seeding time limit, and inactive seeding time limit of a torrent, with the action when a limit is reached and the matching mode. Each limit is "use global", unlimited, or a custom value.
- **Mixed value**: A dialog field whose value is different across the selected torrents.
- **Stalled view**: A torrent view on an open stream that got no data for 2 minutes. It polls qui until data comes again. The other views on the same connection are not affected. A server error is not a stall. _Avoid_: dead stream, stale connection (that is when the whole connection stops, and the browser reconnects).

## Tracker health

- **Tracker health**: qui's verdict on the trackers of one torrent: healthy, unregistered, tracker down, or tracker error. _Avoid_: tracker status.
- **Tracker status**: The status that qBittorrent reports for one tracker of a torrent, such as working, not working, or updating. _Avoid_: tracker health.

## Themes

- **Layout**: The form of the UI that qui shows for the current viewport width: the *mobile layout* when the viewport is less than 768 px wide, or the *desktop layout*. It is not the device type. _Avoid_: device, platform.
- **Theme slot**: One stored theme selection (theme, mode, variation) on the server. There is a default slot and an optional mobile slot. A theme that a browser keeps local is not a slot, because it never reaches the server.

## Client settings

- **Client setting**: A UI preference that qui stores for the user on the server, so it follows the user to every browser and tab, for example the sidebar state, filters, and column layout. _Avoid_: local setting.

## Orphan scan

- **Disc unit**: The folder that holds `BDMV` or `VIDEO_TS`. Orphan scan keeps it whole or reports it whole. A Disc unit with any file in a torrent hides nothing beside it. _Avoid_: Disc (that is the Blu-ray a BDInfo scan reads).
- **Protected path**: A path that orphan scan never reports and never deletes: a file in a torrent, a folder that holds a file in a torrent, a path that an ignore path covers, or a folder that holds an ignore path. Folder cleanup uses the ignore path half of this rule. _Avoid_: Owned path, in-use path, safe path.
- **Partial scan**: An orphan scan that completed at least one selected scan path but could not complete every selected scan path. _Avoid_: Clean scan, failed scan.

## Folder cleanup

- **Leftover folder**: A folder that held a torrent's content and is empty after qui deleted or moved that torrent. A folder that holds only junk files counts as empty. _Avoid_: orphan folder. An abandoned directory is a different thing: orphan scan finds it with a scan.
- **Stop folder**: A folder that the folder cleanup never removes and never climbs past: the default save path, the download path, a category folder in either of them, a category's download folder, or the hardlink base dir. A category's download folder is the folder that qBittorrent downloads the category into, and it can be outside both paths. A folder qBittorrent monitors for `.torrent` files, and the save path set for it, is never removed either, but is not a stop folder: it sets no climb limit of its own. _Avoid_: base dir, root, ceiling.
- **Instance folders**: The folders that qBittorrent saves and downloads into for one instance: the default save path, the download path, the save folder and download folder of each category, and each monitored folder with its save path. Folder cleanup and orphan scan read the same Instance folders. _Avoid_: folder layout, category paths.
- **Folder cleanup**: The step that removes leftover folders after qui deletes or moves a torrent. _Avoid_: prune, managed delete cleanup.

## Release classification

- **Content type**: The kind of media a release name describes: movie, TV, music, audiobook, book, comic, game, app, adult, or unknown. Automations decide it from the name alone; cross-seed also corrects it with the torrent's file sizes. Magazines are books. Courses are unknown: rls gives video courses and book publishers the same type, so no category filter fits them all. When the caller passes no categories, an indexer search picks them from the query and IDs, which is not a content type. _Avoid_: category (a qBittorrent category is a different thing), media type (the disc format read from a RIAJ code).

## Cross-seed search

- **Usable result**: A search hit that survives release and size filtering. Retry passes gate on usable results, never on raw hit counts. _Avoid_: Hit, raw result (when gating is meant).
- **Mixed mode**: One search fan-out where ID-capable indexers receive an ID-only query and the rest receive the title query. Lives in the jackett layer; callers opt in with the `OmitQueryForIDs` flag on an ID-carrying movie or TV request. _Avoid_: Hybrid search, dual query.
- **Arr IDs**: External IDs (imdb/tvdb/tmdb/tvmaze) supplied by a Sonarr/Radarr lookup or its cache. The highest-trust ID source.
- **Tag-sourced IDs**: External IDs read from the Matroska tags of the source file. Trusted blind for querying; a wrong tag is caught by result filtering, not by pre-validation. _Avoid_: MediaInfo IDs (ambiguous with the library name), embedded IDs.
- **Per-indexer retry**: A retry pass that re-queries only the indexers holding no usable result, leaving satisfied indexers untouched. Cross-seed success is per tracker, so one indexer's match never blocks another's retry. The yearless retry is a whole-search retry, not a per-indexer one. _Avoid_: Fallback search, retry-all, rescue pass.
- **Title rescue**: A matching rule, not a search pass. A candidate whose title differs from the source is still accepted when the reported total size is exactly equal and every other release attribute matches. A search applies it only when the setting is on, and Skip recheck turns it off. The Batch matcher always applies it. _Avoid_: Rescue pass, fuzzy match.
- **Query degradation**: A per-search flag telling the frontend the search ran at lower precision than intended (title-only when IDs were wanted). An ID-quality primary, arr- or tag-sourced, is not degraded.
- **Trigger**: How a cross-seed was found: RSS, webhook, seeded search, completion, or interactive apply. Tags, auto-resume, and source filters are set per Trigger. _Avoid_: source (that is the source torrent), request source, mode.
- **Manual match**: A cross-seed apply where the user chooses the target torrent. Candidate discovery and the category and content-type gates are bypassed; the recheck is the arbiter of a wrong pick. _Avoid_: forced match, pinned match.
- **Numbering scheme**: How a TV release names its episode: seasoned (`S04E15`) or absolute (`- 81`, no season). A pair of releases that use the same scheme compare episode numbers directly. _Avoid_: anime numbering, episode format.
- **Episode map**: The Sonarr-sourced triple (season, episode, absolute) for one release name. It lets one seasoned and one absolute release count as the same episode. Exists only when Sonarr names exactly one episode and that episode has an absolute number; otherwise there is no map and the pair falls back to size evidence. _Avoid_: Sonarr mapping, episode translation, absolute lookup.
- **Search scope**: The Torznab indexers and the RED/OPS sites that one search asks for one torrent and its duplicate copies, or the reason that it asks none. Every Trigger reads the same Search scope. A scope with no Torznab indexers is not a Gazelle-only run. _Avoid_: allowed indexers, indexer set.
- **Gazelle-only run**: A library search with Torznab off; candidates come only from the OPS/RED APIs. Reached by the Torznab switch on the Library card, which applies to the next run and is not saved, never inferred from the indexer selection. Needs one Gazelle key. _Avoid_: Torznab-disabled run, forced Gazelle-only.
- **Season numeral**: A Roman numeral at the end of a release title that names the release's season, as in "Kaiju Squad 100 III" for season 3. When the name also has a season marker, the numeral must equal it. When it has none, only II to X count, and only for a pack, not an episode. A movie with a year never has one. Matching reads the title with and without it, and the alternate-title search tries the title without it. _Avoid_: roman suffix, sequel number.
- **Read release**: What qui knows about one release name after it reads the name once: its Title keys, Numbering scheme, season, and absolute episode. Every Match path compares Read releases, never raw names. _Avoid_: release view, parsed release (that is the raw parser output).
- **Title key**: One normalized form of a release title that matching compares, such as the main title, an alternate or AKA title, one side of a slash title, or the title without its Season numeral. Two releases share a title when any of their Title keys are equal. _Avoid_: normalized title, match title.
- **Match path**: One place in qui that decides whether two release names are the same release, such as the webhook check, apply, the season pack check, the Batch matcher, or dedup. Every match path must give the same verdict for a pair unless the corpus lists the difference with a reason. _Avoid_: entry point, matcher (the matcher is the shared rules, not the path).
- **Batch matcher**: The Match path that finds the cross-seeds of torrents already in qBittorrent. The Cross-Seed tab, Filter Cross-Seeds, the delete check, and automations read it. _Avoid_: Local matches, cross-match sets.
- **Match verdict**: The decision of one Match path on a pair of Read releases: no match, or a match class (strict, web source relabel, exact size, title rescue) with the fields that differ. A CRC tag on one name only is never a difference. Two CRC tags that differ are. _Avoid_: Verdict (alone), decision.
- **Match policy**: The switches that a Match path sets on the Match verdict: title rescue, individual episodes, size tolerance, a missing candidate group, no size check, and the season pack matching settings. With a missing candidate group allowed, a group or site that only the local name carries does not count as a difference. The unknown-size preflight of the webhook check allows a missing group. The season pack check allows it too, because a pack name does not carry the group of each episode. Its first pass also has no size check, because its file check compares exact file sizes. The Batch matcher and the content prefilter have no size check for the same reason. Dedup has no size check because it reads names only. _Avoid_: Trigger settings.
- **Match evidence**: What a Match path knows about a pair beyond the two Read releases: sizes, file lists, Sonarr alternate titles, and the episode map. A Match path that lacks one gives the Match verdict less to work with. It does not change the rules. _Avoid_: Evidence (alone), context.
- **Shared file**: One file on disk that two torrents read through the same folder entry, on one instance or on two. A delete with files of one torrent removes it for the other. _Avoid_: same content path (that is path text), content match.
- **Generic HDR tag**: An `HDR` tag in a release name that does not say `HDR10`, `HDR10+`, or `HLG`. Cross-seed matching reads it as `HDR10`, so `DV HDR` matches `DV HDR10`. It stays distinct from `HDR10+` and `HLG`. Release fields and Season pack status keep the tag as written. _Avoid_: plain HDR.

## Indexers

- **Native indexer**: An indexer that qui reaches at the tracker's own Torznab endpoint, with no Prowlarr or Jackett between them. _Avoid_: Direct indexer.
- **Rate-limit cooldown**: The time qui blocks an indexer after it answers 429, for as long as its `Retry-After` header says. _Avoid_: Cooldown (alone), rate limit.

## Cross-seed scans

- **Dir Scan**: The cross-seed search that reads content from folders on disk and searches indexers for releases that match it. It needs no torrent in qBittorrent. _Avoid_: Directory scan (outside UI copy), dirscan.
- **Library Scan**: The cross-seed search that goes through the torrents an instance already seeds and searches indexers for each one. _Avoid_: Seeded search, search run.
- **Searchee**: A top-level folder or file in a Dir Scan directory that Dir Scan tries to match to a release.
- **Webhook scan**: A Dir Scan run that an import webhook starts. The webhook comes from Sonarr, Radarr, Lidarr, or Readarr, or from a script that sends a path. The run scans only the imported folder and ignores the age cutoff. It is not the autobrr webhook that calls `/check` and `/apply`. That webhook is a Trigger. _Avoid_: webhook run, triggered scan.
- **Full scan**: A Dir Scan run of the whole directory, started by hand or by the schedule. _Avoid_: long scan.

## Cross-seed link tree

- **Isolation folder**: A folder that qui adds in the link directory so that the files of one torrent do not mix with the files of other torrents. _Avoid_: hash folder.
- **Linked file**: A file in an added torrent that qui materialized from local data (hardlink or reflink) before the add. _Avoid_: Matched file, existing file.
- **Pending file**: A file in an added torrent that was absent at add time. _Avoid_: Missing file, extra file (when the download is meant).
- **Settle**: The steps after an add that needs a recheck, from the recheck to the resume or the decision to stay paused. _Avoid_: Post-add flow.
- **Alignment**: The renames after an add that make the file and folder names of a torrent match the files that are already on disk. _Avoid_: content path alignment, rename alignment.
- **Add verdict**: The decision for one cross-seed add. It is one of these: refuse with a status, add running, or add paused for Settle. _Avoid_: Add plan (the chosen match), add policy.
- **Pre-add check**: The blocklist check and the check for an existing torrent by v1 and v2 hash. They run before qui builds a link tree for an add. A manual assembly from several episodes skips the blocklist. _Avoid_: Admission (an External Program term).
- **Partial pool**: Related partial cross-seeds that share one source torrent. qui lets one member download at a time and links each completed file into the other members. _Avoid_: Pooled group.
- **Pool member**: One torrent in a partial pool.
- **Pool admission**: The step that makes a new partial add a pool member, and the window after it during which more members can join. _Avoid_: Admission alone (see Admitted Execution and Pre-add check).

## Automations

- **Automation rule**: One saved set of conditions and actions that qui runs against the torrents of one instance on an interval. The UI calls it a workflow. _Avoid_: Workflow (outside UI copy), automation (for one rule).
- **Condition**: A test of one torrent field against a value with an operator. Conditions combine with AND and OR into the condition tree of a rule. _Avoid_: Filter, criterion.
- **Action**: What a rule does to each torrent that its conditions match, for example tag, delete, pause or move. A rule has one or more actions. Delete runs alone. _Avoid_: Effect, step.
- **Shared-file group**: Torrents connected by shared file paths, directly or through another member. _Avoid_: Duplicate group.
- **Delete trigger**: A torrent whose own delete condition matched. This is different from a torrent that a Shared-file group or cross-seed expansion pulls in. _Avoid_: Direct match.
- **Deletion selection**: The torrents that the delete rules of a run choose to remove: the Delete triggers and the torrents that their expansion pulls in. The preview shows the Deletion selection of one rule. _Avoid_: Shared-file group (when referring to selected torrents).
- **Run plan**: What one run of the Automation rules will do: for each Action, the torrents it reaches and the rule that chose each torrent. The live run carries it out. The dry run records it. The Deletion selection is the delete part of the Run plan. The category and tag previews show the plan of one rule. _Avoid_: Desired state, batches.
- **Projected freed bytes**: The estimated space recovered by removing the selected files. Shared paths count once. _Avoid_: Actual freed space.
- **Season pack status**: What `SEASON_PACK_STATUS` reports for one torrent: `pack` for a season pack, `packed` for an episode that a season pack of the same release covers, `unpacked` for an episode with no such pack, empty when the name has no season or more than one season. "Same release" means title, season, cut, other markers, language markers, resolution, source, codec, audio, channels, HDR, and group all match. _Avoid_: Packed status, pack coverage.

## Disc reports

- **Disc**: A Blu-ray as one unit: the folder that holds `BDMV`, or one `.iso`. The unit a BDInfo scan reads. One torrent can hold several Discs. _Avoid_: Blu-ray folder, disc torrent.
- **Disc scan**: One queued or running BDInfo job on one Disc. _Avoid_: BDInfo job, bdinfo run.
- **Disc report**: The cached BDInfo text for one Disc on one instance. _Avoid_: disc info, BDInfo output.
- **Search candidate**: The unit of work in a seeded search run: a source torrent, or a season group formed by season pack automation. A run counts candidates, not torrents. _Avoid_: Torrent (when the count is meant), item.
- **Cross-seed added**: One successful apply into the client. One Search candidate can produce several. _Avoid_: Match, torrent added.
- **Due candidate**: A Search candidate that still needs a search. _Avoid_: Total torrents, pending, remaining.

## Licenses

- **Machine**: One qui config directory on one host. A license activation belongs to a Machine. A new database keeps the Machine but loses its activation record. _Avoid_: Instance (that is a qBittorrent instance), device.
- **Activation**: One license key in use on one Machine. Each key allows a fixed number of Activations. _Avoid_: Seat.
- **License portal**: The website at licenses.getqui.com where a buyer sees their license keys and deactivates a Machine. _Avoid_: Customer portal (that is the Dodo Payments portal).

## Instance configuration

- **Preferences**: qBittorrent's own application preferences, which qui reads and writes through the qBittorrent WebAPI. _Avoid_: Settings (when qBittorrent stores the value).
- **Settings**: Configuration that qui stores itself, for example the tracker reannounce and orphan scan configuration. _Avoid_: Preferences (when qui stores the value).
- **Preference catalog**: The qui list of qBittorrent preferences that says, for each key, where qui shows it and whether a user can change it. _Avoid_: Preference schema, field registry.
- **Network binding**: The network interface and the IP address that qBittorrent uses for peer connections. _Avoid_: Bind interface, outgoing interface.

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

## Page layout

- **Section**: One part of a sectioned page. The user picks it from the section nav of the page, and only one section shows at a time. _Avoid_: tab, panel.
- **Sectioned page**: A page with a section nav, for example Settings or Cross-Seed. _Avoid_: tabbed page.
- **Single page**: A page that stacks its cards and has no section nav, for example Search or Backups.
