---
sidebar_position: 5
title: Orphan Scan
description: Find and remove files not associated with any torrent.
---

import LocalFilesystemDocker from "../_partials/_local-filesystem-docker.mdx";
import OrphanScanDefaultIgnores from "../_partials/_orphan-scan-default-ignores.mdx";

# Orphan Scan

Orphan scan finds and removes files in your download directories that no torrent references.

## How it works

1. qui builds the scan roots from the save paths of your current torrents, not from qBittorrent's default download directory.
2. qui flags files that no torrent references as orphans.
3. Before you confirm deletion, you preview the list.
4. After file deletion, qui removes empty directories.

:::note
When matching paths, qui normalizes Unicode paths to canonical NFC form. If the filesystem and qBittorrent report composed and decomposed forms of the same name, this normalization prevents false orphans. On normalization-sensitive filesystems, qui treats two byte-distinct canonical-equivalent names as one logical path.
:::

:::info
If multiple **active** qBittorrent instances have **[Local Filesystem Access](./instance-settings.md#local-filesystem-access)** enabled and their torrent save paths overlap, qui also protects files that torrents from those other instances reference. qui applies this protection even when it scans a single instance.

To protect files safely, qui must determine whether the scan roots overlap. If any other local-access instance is unreachable or not ready, the scan fails to prevent false positives.
:::

:::warning
qui does not protect disabled instances. If a disabled instance with local filesystem access shares save paths with an active instance, qui can flag its files as orphans. Before you scan, enable the instance or make sure that the paths do not overlap.
:::

<LocalFilesystemDocker />

## Abandoned directories

qui scans a directory only if at least one torrent points to it. If you delete all torrents from a directory, that directory stops being a scan root. qui does not detect leftover files there.

**Example:** You have torrents in `/downloads/old-stuff/`. If you delete all those torrents, orphan scan stops tracking `/downloads/old-stuff/` and does not clean it up.

## Settings

| Setting | Description | Default |
|---------|-------------|---------|
| Grace period | Skip files modified within this window | 10 minutes |
| Ignore paths | Directories to exclude from scanning | - |
| Scan interval | How often scheduled scans run | 24 hours |
| Max files per run | Maximum orphan preview entries saved for a run (also caps what qui can delete from that run) | 1,000 |
| Auto-cleanup | Delete orphans from scheduled scans without manual confirmation | Disabled |
| Max files threshold | If the orphan count is at or below this threshold, auto-delete orphans | 100 |

<OrphanScanDefaultIgnores />

If an ignore path is a scan path, or a directory above a scan path, qui removes that scan path from the run. Use this when a save path is not available on the host that runs qui. If the ignore paths remove all scan paths, the run fails and tells you that the ignore paths cover every scan path.

## Max files per run behavior

1. qui walks all scan roots during each run to keep the scan scope complete.
2. qui sorts the orphan candidates by your selected preview sort.
3. qui applies `Max files per run`. If more candidates exist than the cap, qui marks the run as truncated.
4. qui deletes only the files saved in that run's preview list.

**Example:** If qui finds 2,000 orphan candidates among 5,000 total files and `Max files per run` is 1,000, qui scans all 5,000 files, saves the top 1,000 candidates for preview and deletion, and marks the run as truncated.

### FAQ

**Do I need multiple runs to scan everything?**
No. Each run scans all roots. If orphan candidates exceed the per-run preview cap, delete the files in the current preview first. The next scan then returns the next set of candidates.

## Workflow

1. Trigger a manual or scheduled scan.
2. Review the preview list of orphan files.
3. Confirm deletion.
4. qui deletes the files and removes empty directories.

## Preview features

- **Path column**: Shows the full file path with copy-to-clipboard support.
- **Export CSV**: Downloads the full preview list across all pages as a CSV file.
