---
sidebar_position: 4
title: Orphan Scan
description: Find and remove files not associated with any torrent.
---

import LocalFilesystemDocker from "../_partials/_local-filesystem-docker.mdx";
import OrphanScanDefaultIgnores from "../_partials/_orphan-scan-default-ignores.mdx";

# Orphan Scan

Finds and removes files in your download directories that aren't associated with any torrent.

## How It Works

1. **Scan roots are determined dynamically** - qui scans all unique `SavePath` directories from your current torrents, not qBittorrent's default download directory
2. Files not referenced by any torrent are flagged as orphans
3. You preview the list before confirming deletion
4. Empty directories are cleaned up after file deletion

:::info
If you have multiple **active** qBittorrent instances with `Has local filesystem access` enabled, and their torrent `SavePath` directories overlap, qui also protects files referenced by torrents from those other instances (even when scanning a single instance).

To do this safely, qui must be able to determine whether scan roots overlap. If any other local-access instance is unreachable/not ready, the scan fails to avoid false positives.
:::

:::warning
**Disabled instances are not protected.** If you have a disabled instance with local filesystem access that shares save paths with an active instance, its files may be flagged as orphans. Enable the instance or ensure paths don't overlap before scanning.
:::

<LocalFilesystemDocker />

## Important: Abandoned Directories

Directories are only scanned if at least one torrent points to them. If you delete all torrents from a directory, that directory is no longer a scan root and any leftover files there won't be detected.

**Example:** You have torrents in `/downloads/old-stuff/`. You delete all those torrents. Orphan scan no longer knows about `/downloads/old-stuff/` and won't clean it up.

## Settings

| Setting | Description | Default |
|---------|-------------|---------|
| Grace period | Skip files modified within this window | 10 minutes |
| Ignore paths | Directories to exclude from scanning | - |
| Scan interval | How often scheduled scans run | 24 hours |
| Max files per run | Limit results to prevent overwhelming large scans | 1,000 |
| Auto-cleanup | Automatically delete orphans from scheduled scans | Disabled |
| Auto-cleanup max files | Only auto-delete if orphan count is at or below this threshold | 100 |

<OrphanScanDefaultIgnores />

## Workflow

1. Trigger a scan (manual or scheduled)
2. Review the preview list of orphan files
3. Confirm deletion
4. Files are deleted and empty directories cleaned up

## Preview Features

- **Path column** - Shows the full file path with copy-to-clipboard support
- **Export CSV** - Download the full preview list (all pages) as a CSV file
