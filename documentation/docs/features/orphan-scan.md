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

1. qui builds the scan roots from the save paths of your current torrents. It does not scan qBittorrent's default save path unless you turn on **Scan Default Save Path**.
2. qui flags files that no torrent references as orphans.
3. Before you confirm deletion, you preview the list.
4. qui deletes the files, then removes the directories the preview listed.

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

## Directories no torrent points at

By default, qui scans a directory only if at least one torrent points to it. If you delete all torrents from a directory, that directory stops being a scan root. qui does not detect leftover files there.

**Example:** You have torrents in `/downloads/old-stuff/`. If you delete all those torrents, orphan scan stops tracking `/downloads/old-stuff/` and does not clean it up.

The same gap applies one level up. If your torrents all save to `/data/torrents/mydata/`, a stray `/data/torrents/test.txt` is never walked, because no torrent points at `/data/torrents/`.

## Scanning the default save path

Turn on **Scan Default Save Path** to close that gap. qui reads the default save path from the instance's own qBittorrent settings and walks it as a scan root, including subdirectories that no torrent uses.

Turn on **Including Category Paths** underneath it to also walk every category destination: the save path a category sets, or `default save path / category name` for a category that inherits it. This matters when a category points somewhere outside the default save path, which the first toggle alone would not reach. It is available only while **Scan Default Save Path** is on.

Both are off by default. Turning them on widens what a scan can flag, so review the preview before you confirm a deletion.

Everything else still applies inside the wider roots:

- Files that torrents reference are protected, including torrents on other active instances with local filesystem access.
- Ignore paths, the grace period, and max files per run all apply.
- A save path missing from disk, such as an unmounted volume, is reported rather than treated as scanned, even when a wider root covers it.

If qui cannot read the default save path or the category list from qBittorrent, or qBittorrent reports an empty or relative default save path, the run fails and names the cause. qui does not fall back to a narrower scan, because a narrower scan would report a clean result over a tree you asked it to check.

:::note
These paths come from qBittorrent, so they are paths as qBittorrent sees them. If qui runs in a different container, they must resolve to the same directories on the host that runs qui.
:::

## Abandoned directories

Moves and deletions leave empty directories behind. Orphan scan reports files, so an empty directory is never flagged, and the tree fills up with them over time.

Turn on **Delete Abandoned Directories** to include them. qui reports a directory when this run empties it: either it holds no files at any depth, or every file below it is an orphan the run is about to delete. Each one is listed in the preview alongside the orphan files, marked with a folder icon and no size.

A single file that survives the run keeps its whole chain of parent directories. That covers a file a torrent owns, a file still inside the grace period, a file under your ignore paths, and a file the max-files cap pushed out of this run.

These are never reported, even when empty:

- A scan root itself.
- A category destination, or any directory above one. qBittorrent will save into it again. qui works out a category's folder the same way qBittorrent does, including one that inherits from a parent category.
- Anything under your ignore paths.
- A directory changed more recently than the grace period.
- A directory holding anything qui did not itself list for removal, such as an ignored subdirectory or a symlink.

Directories are removed after the files, so a tree this run empties goes in one pass. qui removes only the directories the preview listed, so the folder count never exceeds what you saw. Anything that changed between the preview and your confirmation is skipped rather than removed: a directory that still holds a file the run kept, one that gained content, or one that has since become a scan root or a category destination is reported as skipped, not failed.

## Settings

| Setting | Description | Default |
|---------|-------------|---------|
| Grace period | Skip files modified within this window | 10 minutes |
| Ignore paths | Directories to exclude from scanning | - |
| Scan interval | How often scheduled scans run | 24 hours |
| Max files per run | Maximum orphan preview entries saved for a run (also caps what qui can delete from that run) | 1,000 |
| Scan default save path | Also walk qBittorrent's default save path, including directories no torrent uses | Disabled |
| Including category paths | Also walk every category destination, explicit or inherited | Disabled |
| Delete abandoned directories | Report directories this run empties | Disabled |
| Auto-cleanup | Delete orphans from scheduled scans without manual confirmation | Disabled |
| Max files threshold | If the orphan file count is at or below this threshold, auto-delete orphans. Directories do not count. | 100 |

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
4. qui deletes the files, then removes the directories you saw in the preview.

## Preview features

- **Path column**: Shows the full file path with copy-to-clipboard support.
- **Export CSV**: Downloads the full preview list across all pages as a CSV file.
