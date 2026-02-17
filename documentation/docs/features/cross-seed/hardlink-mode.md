---
sidebar_position: 3
title: Hardlink Mode
description: Cross-seed using hardlinks or reflinks instead of file renaming.
---

import LocalFilesystemDocker from "../../_partials/_local-filesystem-docker.mdx";

# Hardlink Mode

Hardlink mode is an opt-in cross-seeding strategy that creates a hardlinked copy of the matched files laid out exactly as the incoming torrent expects, then adds the torrent pointing at that hardlink tree. This can make cross-seed alignment simpler and faster, because qBittorrent can start seeding immediately without file rename alignment.

## When to Use

- You want cross-seeds to have their own on-disk directory structure (per tracker / per instance / flat), while still sharing data blocks with the original download.
- You want to avoid qBittorrent rename-alignment and hash rechecks for layout differences.

## Requirements

- Requires **Local filesystem access** on the target qBittorrent instance.
- Hardlink base directory must be on the **same filesystem/volume** as the instance's download paths (hardlinks can't cross filesystems).
- qui must be able to read the instance's content paths and write to the hardlink base directory.

:::tip Multi-filesystem setups
If your downloads span multiple filesystems (e.g., `/mnt/disk1`, `/mnt/disk2`), you can specify **multiple base directories** separated by commas. qui will automatically select the first directory that's on the same filesystem as the source files.

Example: `/mnt/disk1/cross-seed, /mnt/disk2/cross-seed, /mnt/disk3/cross-seed`
:::

<LocalFilesystemDocker />

## Behavior

- Hardlink mode is a **per-instance setting** (not per request). Each qBittorrent instance can have its own hardlink configuration.
- By default, if a hardlink cannot be created (no local access, filesystem mismatch, invalid base dir, etc.), the cross-seed **fails**.
- Enable **"Fallback to regular mode"** to allow failed hardlink operations to fall back to regular cross-seed mode instead of failing. This is useful when files may occasionally be on different filesystems.
- Hardlinked torrents are still categorized using your existing cross-seed category rules (category affix, indexer name, or custom category); the hardlink preset only affects on-disk folder layout.

## Directory Layout

Configure in Cross-Seed → Hardlink Mode → (select instance):

- **Hardlink base directory**: Path(s) on the qui host where hardlink trees are created. For multi-filesystem setups, specify multiple paths separated by commas (e.g., `/mnt/disk1/cross-seed, /mnt/disk2/cross-seed`).
- **Directory preset**:
  - `flat`: `base/TorrentName--shortHash/...`
  - `by-tracker`: `base/<tracker>/TorrentName--shortHash/...`
  - `by-instance`: `base/<instance>/TorrentName--shortHash/...`

### Isolation Folders

For `by-tracker` and `by-instance` presets, qui determines whether an isolation folder is needed based on the torrent's file structure:

- **Torrents with a root folder** (e.g., `Movie/video.mkv`, `Movie/subs.srt`) → files already have a common top-level directory, no isolation folder needed
- **Rootless torrents** (e.g., `video.mkv`, `subs.srt` at top level) → isolation folder added to prevent file conflicts

When an isolation folder is needed, it uses a human-readable format: `<TorrentName--shortHash>` (e.g., `My.Movie.2024.1080p.BluRay--abcdef12`).

For the `flat` preset, an isolation folder is always used to keep each torrent's files separated.

## How to Enable

1. Enable "Local filesystem access" on the qBittorrent instance in Instance Settings.
2. In Cross-Seed → Hardlink Mode, expand the instance you want to configure.
3. Enable "Hardlink mode" for that instance.
4. Set "Hardlink base directory":
   - Single filesystem: `/mnt/data/cross-seed`
   - Multiple filesystems: `/mnt/disk1/cross-seed, /mnt/disk2/cross-seed, /mnt/disk3/cross-seed`
5. Choose a directory preset (`flat`, `by-tracker`, `by-instance`).
6. Optionally enable "Fallback to regular mode" if you want failed hardlinks to use regular cross-seed mode instead of failing.

## Pause Behavior

By default, hardlink-added torrents start seeding immediately (since `skip_checking=true` means they're at 100% instantly). If you want hardlink-added torrents to remain paused, enable the "Skip auto-resume" option for your cross-seed source (Completion, RSS, Webhook, etc.).

## Notes

- Hardlinks share disk blocks with the original file but increase the link count. Deleting one link does not necessarily free space until all links are removed.
- Windows support: folder names are sanitized to remove characters Windows forbids. Torrent file paths themselves still need to be valid for your qBittorrent setup.
- Hardlink mode supports extra files when piece-boundary safe. If the incoming torrent contains extra files not present in the matched torrent (e.g., `.nfo`/`.srt` sidecars), hardlink mode will link the content files and trigger a recheck so qBittorrent downloads the extras. If extras share pieces with content (unsafe), the cross-seed is skipped.

## Reflink Mode (Alternative)

Reflink mode creates copy-on-write clones of the matched files. Unlike hardlinks, reflinks allow qBittorrent to safely modify the cloned files (download missing pieces, repair corrupted data) without affecting the original seeded files.

**Key advantage:** Reflink mode **bypasses piece-boundary safety checks**. This means you can cross-seed torrents with extra/missing files even when those files share pieces with existing content—the clones can be safely modified.

### When to Use Reflink Mode

- You want to cross-seed torrents that hardlink mode would skip due to "extra files share pieces with content"
- Your filesystem supports copy-on-write clones (BTRFS, XFS on Linux; APFS on macOS)
- You prefer the safety of copy-on-write over hardlinks

### Reflink Requirements

- **Local filesystem access** must be enabled on the target qBittorrent instance.
- The base directory must be on the **same filesystem/volume** as the instance's download paths. For multi-filesystem setups, specify multiple paths separated by commas.
- The base directory must be a **real filesystem mount**, not a pooled/virtual mount (common examples: `mergerfs`, other FUSE mounts, `overlayfs`).
- The filesystem must support reflinks:
  - **Linux**: BTRFS, XFS (with reflink=1), and similar CoW filesystems
  - **macOS**: APFS
  - **Windows/FreeBSD**: Not currently supported

:::tip
On Linux, check the filesystem type with `df -T /path` (you want `xfs`/`btrfs`, not `fuseblk`/`fuse.mergerfs`/`overlayfs`).
:::

### Behavior Differences

| Aspect | Hardlink Mode | Reflink Mode |
|--------|--------------|--------------|
| Piece-boundary check | Skips if unsafe | Never skips (safe to modify clones) |
| Recheck | Only when extras exist | Only when extras exist |
| Disk usage | Zero (shared blocks) | Starts near-zero; grows as modified |

### Disk Usage Implications

Reflinks use copy-on-write semantics:
- Initially, cloned files share disk blocks with originals (near-zero additional space)
- When qBittorrent writes to a clone (downloads extras, repairs pieces), only modified blocks are copied
- In worst case (entire file rewritten), disk usage approaches full file size

### How to Enable Reflink Mode

1. Enable "Local filesystem access" on the qBittorrent instance in Instance Settings.
2. In Cross-Seed > Hardlink / Reflink Mode, expand the instance you want to configure.
3. Enable "Reflink mode" for that instance.
4. Set "Base directory":
   - Single filesystem: `/mnt/data/cross-seed`
   - Multiple filesystems: `/mnt/disk1/cross-seed, /mnt/disk2/cross-seed`
5. Choose a directory preset (`flat`, `by-tracker`, `by-instance`).
6. Optionally enable "Fallback to regular mode" if you want failed reflinks to use regular cross-seed mode instead of failing.

:::note
Hardlink and reflink modes are mutually exclusive—only one can be enabled per instance.
:::
