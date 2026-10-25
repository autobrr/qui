---
sidebar_position: 5
title: Cross-seed link directories
sidebar_label: Link Directories
description: How qui lays out hardlink and reflink trees on disk, and how to choose the link directory for each instance.
---

# Link Directories

If you enable **Hardlink mode** or **Reflink mode** for a qBittorrent instance, qui creates a directory tree that matches the expected layout of the incoming torrent. qui then adds the torrent and points it at that tree.

Because these modes set an explicit `savepath` (the link-tree root), qui always disables AutoTMM for torrents added in hardlink or reflink mode.

This applies to:
- Cross-seed searches (RSS, completion, manual, scan)
- Directory scan (dirscan) injections
- Season pack webhook injections (see [Season Packs](./season-packs.md))
- Manual season pack assembly from several episodes (see [Manual Match](./overview.md#manual-match))

## Settings

Configure these options per qBittorrent instance in **Cross-Seed > After injection > Hardlink / Reflink Mode**:

- **Base directories** (`HardlinkBaseDir`): root paths where qui creates link trees. Separate several paths with commas. qui uses a path that is on the same filesystem as the matched source files. When several paths match, qui tries first the path that shares the longest path prefix with the source files. If a link fails with a cross-device error, qui tries the next matching path.
- **Directory organization** (`HardlinkDirPreset`): controls how qui groups trees below the base directory.
- **Fallback to regular mode on error** (`FallbackToRegularMode`): if link-tree creation fails, qui falls back to regular mode instead of failing.

## Directory presets

qui supports three presets:

- `flat`: puts each torrent directly in the base directory
  - Example: `base/Torrent.Name/...`
- `by-tracker`: groups torrents in a folder for each tracker display name
  - Example: `base/TrackerName/Torrent.Name/...`
- `by-instance`: groups torrents in a folder for each instance name
  - Example: `base/MyInstance/Torrent.Name/...`

### Tracker names (by-tracker)

If you use `by-tracker`, qui resolves the folder name with the same fallback chain as cross-seed statistics:

1. **Tracker customization display name** ([Tracker Customizations](../tracker-customizations.md), on the Dashboard under **Tracker Breakdown**)
2. Indexer name (from Prowlarr/Jackett)
3. Raw announce domain

qui sanitizes folder names to make them filesystem-safe.

### Isolation folders

An isolation folder is a folder that qui adds so that the files of one torrent do not mix with the files of other torrents. All presets use the same rule:

- A torrent with a root folder brings its own folder. qui adds no isolation folder.
- A "rootless" torrent (files at the top level) goes into an isolation folder with the torrent name, for example `Torrent.Name`.

The folder that qui adds to the preset directory must be new. If a folder with that name already exists, qui puts the torrent in an isolation folder with the name `<torrent name>--<first 8 characters of the info hash>`, for example `Torrent.Name--abcdef12`. So two torrents with the same name do not share a folder.

qui cuts a long torrent name to 245 bytes, so that the folder name with the hash fits the limit of 255 bytes of most filesystems. qui does not move or rename the folders of torrents that it added before.

## Fallback to regular mode

If you enable **Fallback to regular mode** and link-tree creation fails, qui adds the torrent with a standard `savepath` that points at the matched source files.

If hardlinks fail across filesystem or device boundaries, this fallback prevents injection errors. For example, a pooled mount presents paths that look identical but resolve to different underlying devices.

If no base directory shares a filesystem with the source files, or link creation failed, qui adds the torrent paused and rechecks it. qui starts the torrent after qBittorrent reports 100% complete. For Cross-Seed, **Skip recheck** skips these candidates, except a [byte-complete match](./rules.md#skip-cross-seeds-with-extra-files), which qui adds without a recheck. Dir Scan runs the recheck even when **Skip recheck** is on. Fallbacks for configuration problems (an empty base directory, or no local filesystem access) add the torrent in regular mode with the normal regular-mode rules.

If you disable fallback and link-tree creation fails, qui skips or fails the candidate.
