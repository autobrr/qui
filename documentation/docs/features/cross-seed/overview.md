---
sidebar_position: 1
title: Cross-Seed
description: Automatically cross-seed torrents across trackers.
---

# Cross-Seed Overview

qui finds torrents on other trackers that match content you already seed. It adds them to your qBittorrent instances, so you seed the same content on more than one tracker.

## How It Works

When you cross-seed a torrent, qui:

1. Finds a matching torrent in your library (same content, different tracker)
2. Adds the new torrent and points it to your existing files
3. Applies the correct category and save path

qui supports three modes for file handling:

- **Default mode**: qui reuses the existing files directly. It creates no new files or links. If the incoming torrent has a different folder or file layout, qui must align the names first.
- **Hardlink mode** (optional): qui creates a hardlinked copy of the matched files in the layout that the incoming torrent expects. It then adds the torrent against that tree. This avoids rename alignment.
- **Reflink mode** (optional): qui creates copy-on-write clones (reflinks) of the matched files. qBittorrent can write to or repair the clones without changes to the originals. This makes torrents with extra or missing files safe to cross-seed.

Disc-based media (Blu-ray/DVD) requires manual verification. See [troubleshooting](./troubleshooting.md#blu-ray-or-dvd-cross-seed-left-paused).

## Prerequisites

You need at least one enabled Torznab indexer. Add indexers in **Settings → Indexers**. Click **Discover** to import them from Prowlarr or Jackett, or **Add single** to add a native tracker endpoint by hand. See [Add indexers](../search.md#add-indexers).

Library Scan also runs without Torznab if you configure Gazelle keys for OPS/RED. See [OPS/RED (Gazelle)](./gazelle-ops-red.md).

:::note Prowlarr filters also apply here
A Prowlarr indexer's own search filters, such as freeleech only, also apply to cross-seed searches. See [troubleshooting](./troubleshooting.md#prowlarr-filters-remove-expected-results).
:::

Optional: qui can query OPS/RED directly through tracker Gazelle JSON APIs. This complements Torznab and handles OPS/RED searches even when no Torznab backend is available. When you configure **both** Gazelle keys, qui excludes the OPS/RED Torznab indexers from per-torrent searches. See [OPS/RED (Gazelle)](./gazelle-ops-red.md).

**Optional but recommended:** Configure [Sonarr and Radarr integrations](../search.md#sonarr-and-radarr-integrations) for external ID lookups (IMDb, TMDb, TVDb, TVMaze). ID-based queries return more exact matches, most of all when names differ by locale ("AKA" content).

Without *arr IDs, qui uses a fallback. Some release groups embed IMDb/TMDb/TVDb tags in their MKV files. When a search finds no usable results, qui reads these tags from the torrent's largest `.mkv` file. It then retries indexers that support ID-based search. This fallback requires [Local Filesystem Access](../instance-settings.md#local-filesystem-access) on the instance. qui caches each successful scan per torrent, so it reads the file only once. If a read fails, a later search tries again.

## Discovery Methods

qui provides several ways to find cross-seed opportunities:

### RSS Automation

qui polls tracker RSS feeds on a schedule. Configure this in the **Auto** tab on the Cross-Seed page.

- **Run interval**: How often qui polls the feeds (minimum 30 minutes)
- **Target instances**: The qBittorrent instances that receive cross-seeds
- **Target indexers**: Limit to specific indexers, or use all enabled ones

RSS automation processes the full feed from each selected target indexer on each run. If you select no target indexers, qui uses all enabled indexers.

qui compares each feed title and byte count with eligible local torrents.

qui makes this comparison before it downloads the intended torrent file. RSS does not fetch extra torrent files to measure candidates.

### Library Scan

Library Scan searches other trackers for torrents you already seed. Configure it in the **Scan** tab.

- **Source instance**: The qBittorrent instance to scan
- **Categories/Tags**: Filter which torrents to include
- **Interval**: The delay between torrents. If you enable Torznab, the minimum is 60 seconds. If you disable Torznab and configure Gazelle, the minimum is 5 seconds. Set 10 seconds or more for Gazelle-only runs.
- **Cooldown**: qui skips torrents that it searched within this window (minimum 12 hours). qui records a Torznab cooldown after an indexer completes its search. If you enable Gazelle, qui records a cooldown when it sends a lookup or local checks find nothing to look up. If the search fails before a lookup, qui can try the torrent in the next run.
- **Skip individual episodes**: The run does not search single TV episodes. If [automatic assembly](./season-packs.md#automatic-assembly) is on, groups of episodes still start season pack searches.

#### What the scan card counts

The unit of a run is a search candidate: one source torrent, or one season group that season pack automation formed from episodes you seed.

- **Progress**: Processed candidates out of the due candidates. A due candidate still needs a search. Cooldown and per-indexer search history retire candidates, so the same filters give a smaller total on each later run. The total is not the size of your library.
- **Results**: Each processed candidate lands in one bucket: with cross-seeds, skipped, or failed. The three buckets add up to the processed count when the run completes. A candidate with one added cross-seed and one failed apply counts as with cross-seeds. The failed apply stays in the run details.
- **Cross-seeds added**: Successful adds into the client. One candidate can add several cross-seeds, so this number can be larger than the candidate count.

:::warning
Run this sparingly. The scan touches every matching torrent and queries Torznab and/or Gazelle for each one. Use RSS automation or autobrr for routine coverage. Reserve Library Scan for occasional catch-up passes.
:::

### Auto-Search on Completion

When a torrent finishes downloading, qui starts a cross-seed search. Configure this in the **Auto** tab under "Auto-search on completion".

- **Categories/Tags**: Filter which completed torrents trigger searches
- **Target indexers**: Limit completion searches to specific indexers (empty means all enabled)
- **Exclude categories/tags**: Skip torrents that match these filters
- **Bypass Torznab cache**: If you enable this setting for an instance, completion searches for that instance always run a fresh Torznab search instead of using cached indexer results. Default: off. This setting does not affect Gazelle (OPS/RED) searches, because Gazelle searches do not use the Torznab cache.
- **Search delay**: qui waits 0 to 600 seconds after completion before it searches. Default: 0. If post-completion file moves or sister-torrent injection tools need a head start before qui searches trackers, use this setting.

If a torrent is still **checking** or **moving**, qui waits and runs the completion search afterward. This avoids a search against an unstable path or state.

Completion searches use the same Torznab result classifier as interactive and scheduled searches. An equal positive reported size can activate the same controlled fallback.

### Manual Search

Right-click any torrent in the list to open the cross-seed actions:

- **Search Cross-Seeds**: Query indexers for matching torrents on other trackers
- **Filter Cross-Seeds**: Show torrents in your library that share content with the selected torrent. Use this to identify existing cross-seeds.

Interactive searches, scheduled or on-demand Library Scans, and completion searches use one reported-size rule. Strict release matching always runs first.

An exact positive byte count can relax approved name differences. Reported size is evidence, not proof that the torrent has the same bytes.

qui still checks the downloaded torrent metadata, files, layout, and piece boundaries. Title, season, episode, and split release-group fallbacks require a full recheck.

RSS uses the same classifier with its feed title and byte count. The [autobrr integration](./autobrr.md) uses passive announcement data during `/check`.

Announce matching over alternate titles, such as an anime announced under its English or romaji name, needs a [Sonarr or Radarr integration](../search.md#sonarr-and-radarr-integrations). Without one, qui matches announces on the release name only.

Sonarr also maps a single anime episode between numbering schemes. When Sonarr names exactly one episode for a release, qui reads its season, episode, and absolute number, and stores the map with the cached IDs. A search for an absolute-numbered episode (`Show - 81`) then asks ID-capable indexers for the mapped `S04E15`, and a `S04E15` announce or search result matches a local `Show - 81` file as a strict match, in both directions. The map only adds matches: when a tracker numbers the episode differently from Sonarr, qui treats the pair as an episode mismatch, as before. Season packs are not mapped. See [Season Packs](./season-packs.md#anime-absolute-numbering).

If autobrr has no positive size, qui uses a narrow name-only preflight. This preflight can approve one download, but it cannot approve an add.

### Manual Match

A Manual match is a cross-seed apply where you choose the target torrent yourself. Use it when automatic matching fails, for example when the release name parses as the wrong content type, or when the tracker has no searchable indexer.

Two entry points open the same flow:

- **Add Torrent dialog**: select exactly one `.torrent` file, then click **Match manually**. Magnets, URLs, and multi-file adds cannot use this option.
- **Right-click a torrent** and select **Add Cross-Seed Manually**. The flow opens with that torrent preselected as the target.

qui ranks torrents from the same instance by file-size overlap with the uploaded file. You can also pick any other torrent. A pick with no file overlap shows a warning, but you can proceed.

When the upload parses as a season pack, you can select several episode torrents from the same instance. One selected target uses the existing manual match. If the upload does not parse as a season pack, the dialog permits only one target.

Multiple targets require local filesystem access, hardlink or reflink mode, and a configured link base directory. The dialog shows why assembly is unavailable. Manual assembly works even when season pack automation is off.

The check first selects a complete matching pack alone, if one exists. Otherwise, it suggests the episodes that automatic discovery pairs. You can change the selection or use search to add targets that the proposal list omits. The proposal limit grows with the pack episode count, up to 60 torrents. The dialog reports this limit when it truncates the list.

Before you add the pack, the check shows matched episodes, coverage, missing bytes, rejected targets, and the resolved destination. Coverage uses the episode files in your upload. No coverage threshold applies to manual assembly. Each selected torrent must be complete and provide one playable episode file with an exact size match. Manual selection bypasses title, source-filter, and numbering-scheme checks, but each file still needs a parsed episode identity.

Piece boundary protection also applies to manual assembly in hardlink mode. It blocks the pack when selected and missing files share a data block. Downloading that block can change the original files through hardlinks. The warning links to Cross-seed > Rules, where you can review this protection under Safety & validation.

qui adds the assembled pack paused and rechecks it. It resumes when verified progress reaches the linked byte fraction, with the assembly margin for piece boundaries. If a file moves between check and apply, qui drops that target, reports its name, and assembles the remaining files. Season pack run history records the matched count, coverage, and link mode.

A manual selection bypasses candidate discovery and the category and content-type gates. Link mode per instance settings and tag and category treatment stay the same as the automatic pipeline. Every manual match runs a full recheck before it seeds; you cannot skip it, and it decides a wrong pick. A failed recheck leaves the torrent paused for manual review.

For one selected target, the dialog prefills the category from that torrent and the tags from the cross-seed tag settings. You can edit both. If **Use Custom Category** is on, every cross-seed goes to that one category. The dialog then shows the category and locks it. The save path shows the effective destination and is read-only. With **By Tracker** directory organization, the tracker folder comes from the announce URL in the uploaded file. The tracker does not need a configured indexer.

For multiple targets, season pack category routing supplies the initial category. Your category and tag choices control the add. qui does not append the automatic season pack tags. The destination uses the selected episode files and the instance link directory preset.

### Season Pack Assembly

qui can assemble season-pack torrents from individual episodes you already seed. To use automatic assembly, first enable **Assemble season packs automatically** in the settings; it is disabled by default. When autobrr announces a season pack, qui checks your qBittorrent instances for matching episodes. RSS automation, a cross-seed apply, and Library Scan can also start this flow when you seed only episodes of a pack. qui links the episodes that exist locally. When coverage passes the configured threshold (default 75%), qui adds the pack and qBittorrent downloads the remainder after a recheck. When available, Sonarr, TVDB, and TVMaze improve the threshold decision. This feature requires local filesystem access and hardlink or reflink mode. See [Season Packs](./season-packs.md) for setup.

## Blocklist

Use the per-instance blocklist to stop qui from injecting specific infohashes again.

- **Manage**: Cross-Seed page → Blocklist tab
- **Quick add**: Delete dialog checkbox (appears only for torrents tagged `cross-seed`)

The delete dialog also detects cross-seeds that the deletion affects. This includes [hardlinked copies and ReFS block clones](./hardlink-mode.md#deleting-hardlinked-cross-seeds) on instances with local filesystem access.
