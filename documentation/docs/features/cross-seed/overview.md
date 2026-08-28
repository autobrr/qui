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

If autobrr has no positive size, qui uses a narrow name-only preflight. This preflight can approve one download, but it cannot approve an add.

### Season Pack Assembly

qui can assemble season-pack torrents from individual episodes you already seed. When autobrr announces a season pack, qui checks your qBittorrent instances for matching episodes. RSS automation, a cross-seed apply, and Library Scan can also start this flow when you seed only episodes of a pack. qui links the episodes that exist locally. When coverage passes the configured threshold (default 75%), qui adds the pack and qBittorrent downloads the remainder after a recheck. When available, Sonarr, TVDB, and TVMaze improve the threshold decision. This feature requires local filesystem access and hardlink or reflink mode. See [Season Packs](./season-packs.md) for setup.

## Blocklist

Use the per-instance blocklist to stop qui from injecting specific infohashes again.

- **Manage**: Cross-Seed page → Blocklist tab
- **Quick add**: Delete dialog checkbox (appears only for torrents tagged `cross-seed`)

The delete dialog also detects cross-seeds that the deletion affects. This includes [hardlinked copies and ReFS block clones](./hardlink-mode.md#deleting-hardlinked-cross-seeds) on instances with local filesystem access.
