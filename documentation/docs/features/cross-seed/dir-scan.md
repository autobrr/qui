---
sidebar_position: 4
title: Directory Scanner
description: Scan local directories and automatically cross-seed completed downloads.
---

# Directory Scanner

Directory Scanner (Dir Scan) lets qui scan one or more local folders (movies, TV, etc.) and automatically search for cross-seeds for content it finds on disk.

You configure it in **Cross-Seed → Dir Scan**.

## Requirements

- At least one qBittorrent instance must have **Local filesystem access** enabled.
- Prowlarr or Jackett must be configured (Torznab indexers).
- Optional but recommended: Sonarr/Radarr configured in **Settings → Integrations** for external ID lookups (IMDb/TMDb/TVDb).

## How It Works

For each configured scan directory, qui:

1. Enumerates files in the directory (recursively).
2. Groups files into “searchees” (single-file releases, season folders, etc.).
3. Uses *arr to resolve external IDs when possible.
4. Searches your enabled indexers via Torznab.
5. Matches the incoming torrent’s file list against what’s on disk.
6. If a match is found, adds the torrent to the configured target qBittorrent instance.

### Recheck Behavior

- For **full matches**, Dir Scan adds the torrent with “skip hash check” enabled, so it starts seeding immediately.
- For **partial matches** (when enabled), Dir Scan does **not** skip the hash check, so qBittorrent can verify what you already have and download whatever is missing.

qui skips work when it can:
- It avoids re-searching content that is already present in qBittorrent.
- It avoids downloading torrent files when search results expose an infohash that already exists in qBittorrent.

## Settings (Global)

Open **Dir Scan → Settings**:

- **Match Mode**
  - `Strict`: match by filename + size
  - `Flexible`: match by size only
- **Size Tolerance (%)**: allows small size differences when matching.
- **Minimum Piece Ratio (%)**: when partial matches are enabled, the minimum percent of the torrent’s data that must already exist on disk.
- **Allow partial matches**: allows Dir Scan to add torrents even if the torrent has extra/missing files compared to what’s on disk.
- **Skip piece boundary safety check**: if disabled, qui will refuse partial matches where downloading the missing files could modify pieces that belong to the already-present content (rare, but possible on multi-file torrents).
- **Start torrents paused**: adds injected torrents in paused state.
- **Default Category / Tags**: applied to injected torrents.

## Directories

Each scan directory has its own configuration:

- **Directory Path**: the path qui scans (recursively).
- **qBittorrent Path Prefix**: optional path mapping for container setups (when qui and qBittorrent see different root paths).
- **Target qBittorrent Instance**: where matched torrents are added.
- **Scan Interval (minutes)**: how often to rescan the directory (minimum 60 minutes).
- **Enabled**: enable/disable the directory without deleting it.

## Hardlink/Reflink Modes

If the target qBittorrent instance has hardlink or reflink mode enabled, Dir Scan will use the same behavior as cross-seed:

- Builds a link tree that matches the incoming torrent’s layout.
- Adds the torrent pointing at that tree (`contentLayout=Original`, `skip_checking=true`).

See:
- [Hardlink Mode](hardlink-mode)
- [Link Directories](link-directories)

### Fallback to Regular Mode

When link-tree creation fails (for example: hardlinking across device boundaries), Dir Scan can fall back to regular add behavior **if** the instance has **Fallback to regular mode** enabled. Otherwise, the candidate is skipped/failed.

## Pointing Dir Scan at your *arr library

Dir Scan can scan your Sonarr/Radarr library folders, but be careful with **partial matches**:

- With **Allow partial matches** enabled, qBittorrent may download missing files (or small “extras” like `.nfo`/subtitles) into the folder it’s pointed at. If that folder is your *arr-managed library, you can end up with unexpected extra files alongside your media.
- If you want Dir Scan to be “read-only” for your library, keep **Allow partial matches** disabled (full matches only), and consider disabling **Fallback to regular mode** on the target instance so a hardlink failure can’t cause a torrent to be added directly against your library path.

The safest “set and forget” setup is usually:
- Scan your completed downloads/staging folder, not the final library, and/or
- Use hardlink/reflink mode so cross-seeds live under your configured link-tree base directory.
