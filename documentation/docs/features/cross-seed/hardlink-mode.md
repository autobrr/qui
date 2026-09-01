---
sidebar_position: 3
title: Hardlink Mode
description: Cross-seed with hardlinks or reflinks instead of file renaming.
---

import LocalFilesystemDocker from "../../_partials/_local-filesystem-docker.mdx";

# Hardlink Mode

Hardlink mode is an opt-in cross-seeding strategy. qui creates a hardlinked copy of the matched files in the layout that the incoming torrent expects. qui then adds the torrent and points it to that hardlink tree. qBittorrent starts seeding immediately without file rename alignment, and complete link trees skip hash rechecks.

## When to use

- You want cross-seeds to have their own on-disk directory structure (per tracker, per instance, or flat) while they share data blocks with the original download.
- You want to avoid qBittorrent rename alignment and hash rechecks for layout differences.

## Requirements

- You must enable **Local filesystem access** on the target qBittorrent instance.
- The hardlink base directory must reside on the **same filesystem/volume** as the instance download paths. Hardlinks cannot cross filesystems.
- qui must have permission to read the instance content paths and write to the hardlink base directory.

:::tip Multi-filesystem setups
If your downloads span multiple filesystems (for example `/mnt/disk1` and `/mnt/disk2`), set **multiple base directories** separated by commas. qui selects the first directory that is on the same filesystem as the source files.

Example: `/mnt/disk1/cross-seed, /mnt/disk2/cross-seed, /mnt/disk3/cross-seed`
:::

<LocalFilesystemDocker />

## Behavior

- Hardlink mode is a **per-instance setting**, not a per-request setting. Each qBittorrent instance has its own hardlink configuration.
- Torrents added in hardlink or reflink mode always use an explicit `savepath` (the link-tree root), which turns **AutoTMM off**. If you enable AutoTMM after the add, qBittorrent can move files out of the link tree.
- If qui cannot create a hardlink (due to missing local access, a filesystem mismatch, or an invalid base directory), the cross-seed **fails** by default.
- If you want failed hardlink operations to use regular cross-seed mode instead of failing, enable **"Fallback to regular mode on error"**. Filesystem fallback uses a full recheck. See [troubleshooting](./troubleshooting.md#when-rechecks-are-required-reuse-mode).
- When fallback handles a partial or non-perfect match, qui runs a piece-boundary safety check before it adds the torrent to qBittorrent. qui always enforces this fallback check, even when the **Piece boundary safety check** in **Cross-Seed > Rules > Safety & validation** is off (the default).
- qui categorizes hardlinked torrents with your existing cross-seed category rules (category affix, indexer name, or custom category). The hardlink preset only affects the on-disk folder layout.

## Directory layout

Configure in **Cross-Seed > Rules > Hardlink / Reflink Mode**, then expand the instance:

- **Base directories**: paths on the qui host where qui creates link trees. For multi-filesystem setups, set multiple paths separated by commas (for example `/mnt/disk1/cross-seed, /mnt/disk2/cross-seed`).
- **Directory organization** preset:
  - `flat`: `base/TorrentName--shortHash/...`
  - `by-tracker`: `base/<tracker>/TorrentName--shortHash/...`
  - `by-instance`: `base/<instance>/TorrentName--shortHash/...`

### Isolation folders

For the `by-tracker` and `by-instance` presets, qui inspects the torrent file structure to decide whether it needs an isolation folder:

- **Torrents with a root folder** (for example `Movie/video.mkv` and `Movie/subs.srt`): the files already share a top-level directory, so qui adds no isolation folder.
- **Rootless torrents** (for example `video.mkv` and `subs.srt` at the top level): qui adds an isolation folder to prevent file conflicts.

When qui needs an isolation folder, it uses the format `<TorrentName--shortHash>` (for example `My.Movie.2024.1080p.BluRay--abcdef12`).

For the `flat` preset, qui always uses an isolation folder to keep each torrent's files separate.

## How to enable

1. Enable "Local filesystem access" on the qBittorrent instance in Instance Settings.
2. In **Cross-Seed > Rules > Hardlink / Reflink Mode**, expand the instance you want to configure.
3. Set the **Cross-seed mode** to **Hardlink**.
4. Set **Base directories**:
   - Single filesystem: `/mnt/data/cross-seed`
   - Multiple filesystems: `/mnt/disk1/cross-seed, /mnt/disk2/cross-seed, /mnt/disk3/cross-seed`
5. Choose a **Directory organization** preset (`flat`, `by-tracker`, `by-instance`).
6. If you want failed hardlinks to use regular cross-seed mode instead of failing, enable **"Fallback to regular mode"**.

## Pause behavior

By default, hardlink-added torrents start seeding immediately because `skip_checking=true` sets them to 100%. If you want hardlink-added torrents to stay paused, disable the "Auto-resume after injection" toggle for your cross-seed source under **Cross-Seed > Rules > Post-injection behavior**.

When hardlink or reflink mode creates a complete link tree with no extra files to download, qui adds the torrent with hash checking skipped and does not trigger an automatic recheck. If qBittorrent reports `missing files`, see [Hardlink/reflink cross-seed shows "missing files"](./troubleshooting.md#hardlinkreflink-cross-seed-shows-missing-files).

When the incoming torrent has extra files that are not present in the matched torrent, qui adds the torrent paused and triggers a recheck. If the recheck confirms that the missing data fits within the **Max auto-start download** limit, qui resumes the torrent. When only ignorable files are missing (samples, `.nfo`, subtitles), qui resumes anyway, up to 200 MiB (see [Rules](./rules.md#max-auto-start-download)).

If hardlink or reflink mode falls back to regular mode for a partial or non-perfect match, the fallback add is stricter. qui checks piece boundaries first. If the check passes, qui adds the torrent in a paused state. Safe fallback adds require a full 100% recheck before auto-resume.

### Pooled Partial Completion

Enable **Automatically pool torrents with extra data** under Cross-Seed → Rules → Hardlink / Reflink Mode. The checkbox controls one global setting, remains available regardless of the per-instance mode selections, is off by default, and applies only to new partial hardlink or reflink additions; qui does not import existing partial torrents into pools.

qui groups related members persistently by their original source torrent identity, not category, release name, save path, or the latest match. Each member keeps the managed link-tree root chosen when it was added, and unfinished work resumes after a qui restart.

After each recheck, **Max auto-start download** under Rules → After injection → Post-injection behavior determines whether a member may acquire missing data. Over-budget members remain paused and can be reconsidered when another member supplies files or the limit changes. The existing sidecar allowance for samples, `.nfo` files, and subtitles still applies.

Only one member downloads in a pool at a time. When a whole wanted file completes, qui can link or clone it into stopped related members, recheck each target, and resume only targets that qBittorrent verifies as complete. Exact file evidence is required; ambiguous names, moved roots, changed priorities, conflicting targets, and unsafe paths remain paused for review.

If the active member's downloaded-byte counter makes no progress for 15 minutes, qui pauses it, retains all partial data, and gives it a 30-minute cooldown before retry. Another eligible member may run during that cooldown. Disabling pooled completion pauses only a downloader qui started; persisted pool state remains available if the setting is enabled again.

Hardlink members require every originally linked file to verify completely before acquisition because repair could modify the source through the shared inode. A propagated hardlink that fails verification is removed only when the current qui process can prove it created that exact link; otherwise the member stays paused for manual recovery. Reflink members can safely repair incomplete clones through copy-on-write, so failed target verification keeps the clone and can return the member to normal pooled acquisition.

## Notes

- Hardlinks share disk blocks with the original file but increase the link count. Deleting one link does not free space until you remove all remaining links.
- Windows support: qui sanitizes folder names to remove characters that Windows forbids. The torrent file paths must remain valid for your qBittorrent setup.
- If extra files are piece-boundary safe, hardlink mode supports them. If the incoming torrent contains extra files that the matched torrent lacks (for example `.nfo` or `.srt` sidecars), hardlink mode links the content files and triggers a recheck so qBittorrent downloads the extras. If the extras share pieces with content (unsafe) and the **Piece boundary safety check** is enabled, qui skips the cross-seed.
- In Dir Scan, partial matches (for example season packs where only some episodes exist on disk) require the **Download missing files** setting in [Dir Scan settings](./dir-scan.md#settings-global). Without it, Dir Scan rejects partial link tree injections. Other sources add partial season packs paused and recheck them.

## Deleting Hardlinked Cross-Seeds

The delete dialog cross-seed check detects hardlinked copies by verifying filesystem identity, even when copies live in a different save path. Detected copies appear in the affected list with a **Hardlink** badge. You can delete them together with the selection with **Also delete these cross-seeded torrents**.

Because hardlinked copies keep their own links to the data, deleting the original torrent files does not break the copies and does not free disk space. The filesystem reclaims disk space only after you remove all remaining links. The dialog warning text reflects this.

Detection requires:

- **Local filesystem access** enabled on the instances. qui must run in an environment where qBittorrent save paths are valid, identical to the requirements above. Without local access, qui detects only same-path cross-seeds.
- The copy torrent name must match the original by name or release metadata. qui does not detect copies renamed beyond recognition.

If qui cannot verify a copy (for example when its file list is temporarily unavailable), the check fails visibly instead of reporting "no cross-seeds found".

On Windows, the same delete check also detects ReFS block-cloned files and displays a **Reflink** badge. This is a bounded verification step, not a disk scan:

- qui considers only torrents that match by exact name or release metadata.
- qui examines only the files that qBittorrent lists for those torrents. qui does not enumerate the volume, scan unrelated directories, query block reference counts, or calculate reclaimable space.
- Source and candidate files must reside on the same ReFS volume and report block-cloning support.
- qui pairs files by exact case-insensitive normalized relative path and qBittorrent-reported size. If layouts differ and the basename-and-size key is unique in both torrents, qui uses an exact basename-and-size fallback.
- qui skips zero-length files, missing files, symbolic and reparse-point links, directories, and unsafe traversal paths.

The ReFS check compares current allocated disk extents. It proves that the files share at least one allocated cluster at the time of the check. It does not prove clone history, and it does not prove that the files still share every byte. Copy-on-write modifications can leave only part of a file shared. If a process rewrites all formerly shared clusters, qui cannot detect the clone.

Files smaller than one ReFS allocation cluster have no cloned extent, because qui's Windows reflink implementation copies the non-cluster-aligned tail. Those files cannot provide ReFS shared-extent evidence.

## Reflink Mode (Alternative)

Reflink mode creates copy-on-write clones of the matched files. Unlike hardlinks, reflinks allow qBittorrent to modify the cloned files (download missing pieces, repair corrupted data) without any effect on the original seeded files.

**Key advantage:** reflink mode **bypasses piece-boundary safety checks**. You can cross-seed torrents with extra or missing files even when those files share pieces with existing content, because qBittorrent can modify the clones safely.

### When to use reflink mode

- You want to cross-seed torrents that hardlink mode skips with "extra files share pieces with content"
- Your filesystem supports copy-on-write clones (BTRFS or XFS on Linux, APFS on macOS, ReFS on Windows)
- You prefer the safety of copy-on-write over hardlinks

### Reflink Requirements

- You must enable **Local filesystem access** on the target qBittorrent instance.
- The base directory must reside on the **same filesystem/volume** as the instance download paths. For multi-filesystem setups, set multiple paths separated by commas.
- The base directory must be a **real filesystem mount**, not a pooled or virtual mount (common examples: `mergerfs`, other FUSE mounts, `overlayfs`).
- The filesystem must support reflinks:
  - **Linux**: BTRFS, XFS (with reflink=1), and similar copy-on-write filesystems
  - **macOS**: APFS
  - **Windows**: ReFS on the same volume as the source files and reflink base directory
  - **FreeBSD**: not supported

:::note
Windows reflink mode uses ReFS block cloning and requires a ReFS filesystem. qui does not support NTFS. If the matched source path is a symlink, qui resolves it before cloning. The resolved source and the reflink base directory must reside on the same ReFS volume. If reflink creation fails, fallback depends on the "Fallback to regular mode" setting.
:::

:::tip
On Linux, verify the filesystem type with `df -T /path`. You want `xfs` or `btrfs`, not `fuseblk`, `fuse.mergerfs`, or `overlayfs`.
:::

### Behavior differences

| Aspect | Hardlink mode | Reflink mode |
|--------|--------------|--------------|
| Piece-boundary check | Skips if unsafe when the **Piece boundary safety check** is enabled | Never skips (clones are safe to modify) |
| Recheck | Only when extras or disc layouts require verification | Only when extras or disc layouts require verification |
| Disk usage | Zero (shared blocks) | Starts near zero, grows as modified |

### Disk usage implications

Reflinks use copy-on-write semantics:
- Cloned files initially share disk blocks with the originals, so they require near-zero extra space.
- When qBittorrent writes to a clone (downloads extras, repairs pieces), the filesystem copies only the modified blocks.
- If qBittorrent rewrites the entire file in the worst case, disk usage approaches the full file size.

### How to enable reflink mode

1. Enable "Local filesystem access" on the qBittorrent instance in Instance Settings.
2. In **Cross-Seed > Rules > Hardlink / Reflink Mode**, expand the instance you want to configure.
3. Set the **Cross-seed mode** to **Reflink (copy-on-write)**.
4. Set **Base directories**:
   - Single filesystem: `/mnt/data/cross-seed`
   - Multiple filesystems: `/mnt/disk1/cross-seed, /mnt/disk2/cross-seed`
5. Choose a **Directory organization** preset (`flat`, `by-tracker`, `by-instance`).
6. If you want failed reflinks to use regular cross-seed mode instead of failing, enable **"Fallback to regular mode"**.

:::note
Hardlink and reflink modes are mutually exclusive. You can enable only one per instance.
:::
