---
sidebar_position: 5
title: Link Directories
description: How qui lays out hardlink/reflink trees on disk.
---

# Link Directories

When **Hardlink mode** or **Reflink mode** is enabled for a qBittorrent instance, qui creates a directory tree that matches the incoming torrent’s expected layout, then adds the torrent pointing at that tree.

This applies to:
- Cross-seed searches (RSS, completion, manual, scan)
- Directory scan (dirscan) injections

## Settings

Configured per qBittorrent instance in **Cross-Seed → Hardlink Mode**:

- **Base directory** (`HardlinkBaseDir`): root path where link trees are created.
- **Directory preset** (`HardlinkDirPreset`): controls how trees are grouped below the base directory.
- **Fallback to regular mode** (`FallbackToRegularMode`): if link-tree creation fails, qui can fall back to “regular mode” instead of skipping/failing.

## Directory Presets

qui supports three presets:

- `flat`: one folder per torrent under the base directory
  - Example: `base/Torrent.Name--abcdef12/...`
- `by-tracker`: groups by tracker display name, then optional isolation folder
  - Example: `base/TrackerName/Torrent.Name--abcdef12/...`
- `by-instance`: groups by instance name, then optional isolation folder
  - Example: `base/MyInstance/Torrent.Name--abcdef12/...`

### Tracker Names (by-tracker)

For `by-tracker`, qui resolves the folder name using the same fallback chain as cross-seed statistics:

1. **Tracker customization display name** (Settings → Tracker Customizations)
2. Indexer name (from Prowlarr/Jackett)
3. Raw announce domain

Folder names are sanitized to be filesystem-safe.

### Isolation Folders

For `by-tracker` and `by-instance`, qui adds an isolation folder only when needed:

- Torrents with a common root folder don’t need isolation.
- “Rootless” torrents (top-level files) use an isolation folder to avoid collisions.

For `flat`, an isolation folder is always used.

## Fallback to Regular Mode

If **Fallback to regular mode** is enabled, qui will fall back to adding the torrent with a normal `savepath` (pointing at the matched source files) when link-tree creation fails.

This is particularly useful when hardlinking can intermittently fail due to filesystem/device boundaries (for example: pooled mounts where two paths look the same but resolve to different underlying devices).

If fallback is disabled, qui skips/fails the candidate when link-tree creation fails.
