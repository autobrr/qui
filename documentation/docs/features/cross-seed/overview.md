---
sidebar_position: 1
title: Cross-Seed
description: Automatically cross-seed torrents across trackers.
---

# Cross-Seed Overview

qui includes intelligent cross-seeding capabilities that help you automatically find and add matching torrents across different trackers. This allows you to seed the same content on multiple trackers.

## How It Works

When you cross-seed a torrent, qui:

1. Finds a matching torrent in your library (same content, different tracker)
2. Adds the new torrent pointing to your existing files
3. Applies the correct category and save path automatically

qui supports three modes for handling files:

- **Default mode**: Reuses existing files directly. No new files or links are created. May require rename-alignment if the incoming torrent has a different folder/file layout.
- **Hardlink mode** (optional): Creates a hardlinked copy of the matched files laid out exactly as the incoming torrent expects, then adds the torrent pointing at that tree. Avoids rename-alignment entirely.
- **Reflink mode** (optional): Creates copy-on-write clones (reflinks) of the matched files. Allows safe cross-seeding of torrents with extra/missing files because qBittorrent can write/repair the clones without affecting originals.

Disc-based media (Blu-ray/DVD) requires manual verification. See [troubleshooting](./troubleshooting.md#blu-ray-or-dvd-cross-seed-left-paused).

## Prerequisites

You need Prowlarr or Jackett to provide Torznab indexer feeds. Add your indexers in **Settings → Indexers** using the "1-click sync" feature to import from Prowlarr/Jackett automatically.

:::note Prowlarr filters also apply here
A Prowlarr indexer's own search filters, such as freeleech only, also apply to cross-seed searches. See [troubleshooting](./troubleshooting.md#prowlarr-filters-remove-expected-results).
:::

Optional: qui can also query OPS/RED directly via the trackers' Gazelle JSON APIs. This complements Torznab, can handle OPS/RED searches even when no Torznab backend is available, and excludes OPS/RED Torznab indexers for per-torrent searches only when **both** Gazelle keys are configured. See [OPS/RED (Gazelle)](./gazelle-ops-red.md).

**Optional but recommended:** Configure Sonarr/Radarr instances in **Settings → Integrations** to enable external ID lookups (IMDb, TMDb, TVDb, TVMaze). When configured, qui queries your *arr instances to resolve IDs for cross-seed searches, improving match accuracy on indexers that support ID-based queries.
- This is especially helpful for content that is "AKA" type, and can have differing names depending on locale.

Without *arr IDs, qui has a fallback. Some release groups embed IMDb/TMDb/TVDb tags in their MKV files. When a search finds no usable results, qui reads these tags from the torrent's largest `.mkv` file and retries the indexers that support ID-based search. This fallback needs [Local Filesystem Access](../instance-settings.md#local-filesystem-access) on the instance. qui caches each successful scan per torrent, so it reads the file only once. If a read fails, a later search tries again.

## Discovery Methods

qui offers several ways to find cross-seed opportunities:

### RSS Automation

Scheduled polling of tracker RSS feeds. Configure in the **Auto** tab on the Cross-Seed page.

- **Run interval** - How often to poll feeds (minimum 30 minutes)
- **Target instances** - Which qBittorrent instances receive cross-seeds
- **Target indexers** - Limit to specific indexers or use all enabled ones

RSS automation processes the full feed from each selected target indexer on each run. If no target indexers are selected, it uses all enabled indexers.

It compares each feed title and byte count with eligible local torrents.

This comparison happens before the one intended torrent-file download. RSS does not fetch extra torrent files to measure candidates.

### Library Scan

Deep scan of torrents you already seed to find cross-seed opportunities on other trackers. Configure in the **Scan** tab.

- **Source instance** - The qBittorrent instance to scan
- **Categories/Tags** - Filter which torrents to include
- **Interval** - Delay between processing each torrent (minimum 60 seconds with Torznab enabled; minimum 5 seconds when Torznab is disabled and Gazelle is configured; recommended 10+ seconds for Gazelle-only runs)
- **Cooldown** - Skip torrents searched within this window (minimum 12 hours). qui records this only after an actual remote Gazelle or Torznab request, so local preflight failures and local Gazelle skips do not suppress future searches.
- **Skip individual episodes** - The run does not search single TV episodes. Groups of episodes still start season pack searches when [automatic assembly](./season-packs.md#automatic-assembly) is on.

:::warning
Run sparingly. This deep scan touches every matching torrent and queries Torznab and/or Gazelle for each one. Use RSS automation or autobrr for routine coverage; reserve library scan for occasional catch-up passes.
:::

### Auto-Search on Completion

Triggers a cross-seed search when torrents finish downloading. Configure in the **Auto** tab under "Auto-search on completion".

- **Categories/Tags** - Filter which completed torrents trigger searches
- **Target indexers** - Limit completion searches to specific indexers (empty means all enabled)
- **Exclude categories/tags** - Skip torrents matching these filters
- **Bypass Torznab cache** - When enabled for an instance, completion searches for that instance always perform a fresh Torznab search instead of using cached indexer results. Default: off. Does not affect Gazelle (OPS/RED) searches, which do not use the Torznab cache.
- **Search delay** - Wait 0-600 seconds after completion before searching. Default: 0. Use this when post-completion file moves or sister-torrent injection tools need a short head start before qui searches trackers.

If a torrent is still **checking** or **moving**, qui waits and runs the completion search afterward instead of searching immediately against an unstable path/state.

Completion searches use the same Torznab result classifier as interactive and scheduled searches. An equal positive reported size can activate the same controlled fallback.

### Manual Search

Right-click any torrent in the list to access cross-seed actions:

- **Search Cross-Seeds** - Query indexers for matching torrents on other trackers
- **Filter Cross-Seeds** - Show torrents in your library that share content with the selected torrent (useful for identifying existing cross-seeds)

Interactive searches, scheduled or on-demand Library Scans, and completion searches use one reported-size rule. Strict release matching always runs first.

An exact positive byte count can relax approved name differences. Reported size is evidence, not proof that the torrent has the same bytes.

qui still checks the downloaded torrent metadata, files, layout, and piece boundaries. Title, season, episode, and split release-group fallbacks require a full recheck.

RSS uses the same classifier with its feed title and byte count. The [autobrr integration](./autobrr.md) uses passive announcement data during `/check`.

If autobrr has no positive size, qui uses a narrow name-only preflight. This preflight can approve one download, but it cannot approve an add.

### Season Pack Assembly

Assemble season-pack torrents from individual episodes you already seed. When autobrr announces a season pack, qui checks your qBittorrent instances for matching episodes, links whatever is already local, and lets qBittorrent download the remainder after recheck when coverage passes the configured threshold (default 75%). Sonarr, TVDB, and TVMaze improve the threshold decision when available. Requires local filesystem access and hardlink/reflink mode. See [Season Packs](./season-packs.md) for setup.

## Blocklist

Use the per-instance blocklist to prevent specific infohashes from being injected again.

- **Manage**: Cross-Seed page → Blocklist tab
- **Quick add**: Delete dialog checkbox (only shown for torrents tagged `cross-seed`)

The delete dialog can also detect cross-seeds that would be affected by the deletion, including [hardlinked copies and ReFS block clones](./hardlink-mode.md#deleting-hardlinked-cross-seeds) on instances with local filesystem access.
