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

:::note
qui normalizes Unicode paths to canonical NFC form during matching. This avoids false orphans when equivalent composed/decomposed names are reported differently. On normalization-sensitive filesystems, two byte-distinct canonical-equivalent names are treated as one logical path.
:::

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
| Max files per run | Maximum orphan preview entries saved for a run (also caps what can be deleted from that run) | 1,000 |
| Auto-cleanup | Automatically delete orphans from scheduled scans | Disabled |
| Auto-cleanup max files | Only auto-delete if orphan count is at or below this threshold | 100 |

<OrphanScanDefaultIgnores />

## Max Files Per Run Behavior

- Scan scope is still full: qui walks all scan roots each run.
- Then it sorts orphan candidates by your selected preview sort.
- Then it applies `Max files per run` and marks the run as truncated when more candidates exist.
- Deletion only operates on files saved in that run's preview list.

**Example:** If 5,000 files are scanned, 2,000 are orphan candidates, and `Max files per run` is 1,000, qui scans all 5,000, saves the top 1,000 candidates for preview/deletion, and marks the run truncated.

### FAQ

**Do I need multiple runs to scan everything?**
No. Each run scans all roots. Multiple runs are only needed if you want to work through orphan candidates beyond the per-run preview cap.

## Workflow

1. Trigger a scan (manual or scheduled)
2. Review the preview list of orphan files
3. Confirm deletion
4. Files are deleted and empty directories cleaned up

## Preview Features

- **Path column** - Shows the full file path with copy-to-clipboard support
- **Export CSV** - Download the full preview list (all pages) as a CSV file
