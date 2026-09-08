---
sidebar_position: 5
title: Link Directories
description: How qui lays out hardlink/reflink trees on disk.
---

# Link Directories

If you enable **Hardlink mode** or **Reflink mode** for a qBittorrent instance, qui creates a directory tree that matches the expected layout of the incoming torrent. qui then adds the torrent and points it at that tree.

Because these modes set an explicit `savepath` (the link-tree root), qui always disables AutoTMM for torrents added in hardlink or reflink mode.

This applies to:
- Cross-seed searches (RSS, completion, manual, scan)
- Directory scan (dirscan) injections
- Season pack webhook injections (see [Season Packs](./season-packs.md))

## Settings

Configure these options per qBittorrent instance in **Cross-Seed > Rules > Hardlink / Reflink Mode**:

- **Base directories** (`HardlinkBaseDir`): root paths where qui creates link trees. Separate several paths with commas. qui uses the first path that is on the same filesystem as the matched source files.
- **Directory organization** (`HardlinkDirPreset`): controls how qui groups trees below the base directory.
- **Fallback to regular mode on error** (`FallbackToRegularMode`): if link-tree creation fails, qui falls back to regular mode instead of failing.

## Directory presets

qui supports three presets:

- `flat`: one folder per torrent under the base directory
  - Example: `base/Torrent.Name--abcdef12/...`
- `by-tracker`: groups by tracker display name, then an optional isolation folder
  - Example: `base/TrackerName/Torrent.Name--abcdef12/...`
- `by-instance`: groups by instance name, then an optional isolation folder
  - Example: `base/MyInstance/Torrent.Name--abcdef12/...`

### Tracker names (by-tracker)

If you use `by-tracker`, qui resolves the folder name with the same fallback chain as cross-seed statistics:

1. **Tracker customization display name** ([Tracker Customizations](../tracker-customizations.md), on the Dashboard under **Tracker Breakdown**)
2. Indexer name (from Prowlarr/Jackett)
3. Raw announce domain

qui sanitizes folder names to make them filesystem-safe.

### Isolation folders

If you use `by-tracker` or `by-instance`, qui adds an isolation folder only when needed:

- Torrents with a common root folder do not need isolation.
- "Rootless" torrents (top-level files) use an isolation folder to avoid collisions.

If you use `flat`, qui always uses an isolation folder.

## Fallback to regular mode

If you enable **Fallback to regular mode** and link-tree creation fails, qui adds the torrent with a standard `savepath` that points at the matched source files.

If hardlinks fail across filesystem or device boundaries, this fallback prevents injection errors. For example, a pooled mount presents paths that look identical but resolve to different underlying devices.

If no base directory shares a filesystem with the source files, or link creation failed, qui adds the torrent paused and rechecks it. qui starts the torrent after qBittorrent reports 100% complete. For Cross-Seed, **Skip recheck** skips these candidates. Dir Scan runs the recheck even when **Skip recheck** is on. Fallbacks for configuration problems (an empty base directory, or no local filesystem access) add the torrent in regular mode with the normal regular-mode rules.

If you disable fallback and link-tree creation fails, qui skips or fails the candidate.
