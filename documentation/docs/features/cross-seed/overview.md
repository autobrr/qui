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

Disc-based media (Blu-ray/DVD) requires manual verification. See [troubleshooting](troubleshooting#blu-ray-or-dvd-cross-seed-left-paused).

## Prerequisites

You need Prowlarr or Jackett to provide Torznab indexer feeds. Add your indexers in **Settings → Indexers** using the "1-click sync" feature to import from Prowlarr/Jackett automatically.

Optional: qui can also query OPS/RED via the trackers' Gazelle JSON APIs. This complements Torznab (and excludes OPS/RED Torznab indexers for per-torrent searches when Gazelle is enabled). See [OPS/RED (Gazelle)](gazelle-ops-red).

**Optional but recommended:** Configure Sonarr/Radarr instances in **Settings → Integrations** to enable external ID lookups (IMDb, TMDb, TVDb, TVMaze). When configured, qui queries your *arr instances to resolve IDs for cross-seed searches, improving match accuracy on indexers that support ID-based queries.

## Discovery Methods

qui offers several ways to find cross-seed opportunities:

### RSS Automation

Scheduled polling of tracker RSS feeds. Configure in the **Auto** tab on the Cross-Seed page.

- **Run interval** - How often to poll feeds (minimum 30 minutes)
- **Target instances** - Which qBittorrent instances receive cross-seeds
- **Target indexers** - Limit to specific indexers or use all enabled ones

RSS automation processes the full feed from every enabled indexer on each run, matching against torrents across your target instances.

### Library Scan

Deep scan of torrents you already seed to find cross-seed opportunities on other trackers. Configure in the **Scan** tab.

- **Source instance** - The qBittorrent instance to scan
- **Categories/Tags** - Filter which torrents to include
- **Interval** - Delay between processing each torrent (minimum 60 seconds with Torznab enabled; minimum 1 second when Torznab is disabled and Gazelle is configured)
- **Cooldown** - Skip torrents searched within this window (minimum 12 hours)

:::warning
Run sparingly. This deep scan touches every matching torrent and queries Torznab and/or Gazelle for each one. Use RSS automation or autobrr for routine coverage; reserve library scan for occasional catch-up passes.
:::

### Auto-Search on Completion

Triggers a cross-seed search when torrents finish downloading. Configure in the **Auto** tab under "Auto-search on completion".

- **Categories/Tags** - Filter which completed torrents trigger searches
- **Target indexers** - Limit completion searches to specific indexers (empty means all enabled)
- **Exclude categories/tags** - Skip torrents matching these filters

### Manual Search

Right-click any torrent in the list to access cross-seed actions:

- **Search Cross-Seeds** - Query indexers for matching torrents on other trackers
- **Filter Cross-Seeds** - Show torrents in your library that share content with the selected torrent (useful for identifying existing cross-seeds)

## Blocklist

Use the per-instance blocklist to prevent specific infohashes from being injected again.

- **Manage**: Cross-Seed page → Blocklist tab
- **Quick add**: Delete dialog checkbox (only shown for torrents tagged `cross-seed`)
