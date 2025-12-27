# Cross-Seeding in qui

## How It Works

When you cross-seed a torrent, qui:
1. Finds a matching torrent in your library (same content, different tracker)
2. Adds the new torrent pointing to your existing files
3. Applies the correct category and save path automatically

qui supports three modes for handling files:

- **Default mode**: Reuses existing files directly. No new files or links are created. May require rename-alignment if the incoming torrent has a different folder/file layout.
- **Hardlink mode** (optional): Creates a hardlinked copy of the matched files laid out exactly as the incoming torrent expects, then adds the torrent pointing at that tree. Avoids rename-alignment entirely.
- **Reflink mode** (optional): Creates copy-on-write clones (reflinks) of the matched files. Allows safe cross-seeding of torrents with extra/missing files because qBittorrent can write/repair the clones without affecting originals.

## Hardlink Mode (optional)

Hardlink mode is an opt-in strategy that creates a hardlinked file tree matching the incoming torrent's expected layout. The torrent is then added with `savepath` pointing to that tree, `contentLayout=Original`, and `skip_checking=true`, so qBittorrent can start seeding immediately without a hash recheck. By forcing `contentLayout=Original`, hardlink mode ensures the on-disk layout matches the incoming torrent exactly, regardless of the instance's default content layout preference.

Hardlink mode is configured **per-instance**, allowing you to enable it only for instances where qui has local filesystem access.

### When to use

- You want cross-seeds to have their own on-disk directory structure (per tracker / per instance / flat), while still sharing data blocks with the original download.
- You want to avoid rename-alignment and hash rechecks caused by layout differences between torrents.

### Requirements

- **Local filesystem access** must be enabled on the target qBittorrent instance (Instance Settings > "Local filesystem access").
- The hardlink base directory must be on the **same filesystem/volume** as the instance's download paths (hardlinks cannot cross filesystems).

### Failure behavior

If hardlink mode is enabled for an instance and a hardlink cannot be created (no local access, filesystem mismatch, invalid base directory, permissions issue, etc.), the cross-seed **fails**. There is no fallback to default mode.

### Directory presets

Configure in Cross-Seed > Hardlink Mode for each instance:

- **Hardlink base directory**: Path on the qui host where hardlink trees are created.
- **Directory preset**:
  - `flat`: Always creates an isolation folder: `<base>/<TorrentName--shortHash>/...`
  - `by-tracker`: `<base>/<incoming-tracker-display-name>/...` (isolation folder added when needed)
  - `by-instance`: `<base>/<instance-name>/...` (isolation folder added when needed)

For `by-tracker`, the "incoming tracker display name" is resolved using your Tracker Customizations (Dashboard > Tracker Breakdown) when available; otherwise it falls back to the tracker domain or indexer name.

#### Isolation folders

For `by-tracker` and `by-instance` presets, qui determines whether an isolation folder is needed based on the torrent's file structure:

- **Torrents with a root folder** (e.g., `Movie/video.mkv`, `Movie/subs.srt`) → files already have a common top-level directory, no isolation folder needed
- **Rootless torrents** (e.g., `video.mkv`, `subs.srt` at top level) → isolation folder added to prevent file conflicts

When an isolation folder is needed, it uses a human-readable format: `<TorrentName--shortHash>` (e.g., `My.Movie.2024.1080p.BluRay--abcdef12`).

For the `flat` preset, an isolation folder is always used to keep each torrent's files separated.

### How to enable

1. Enable "Local filesystem access" on the qBittorrent instance in Instance Settings.
2. In Cross-Seed > Hardlink Mode, expand the instance you want to configure.
3. Enable "Hardlink mode" for that instance.
4. Set "Hardlink base directory" to a path on the same filesystem as your downloads.
5. Choose a directory preset (`flat`, `by-tracker`, `by-instance`).

### Pause behavior

By default, hardlink-added torrents start seeding immediately (since `skip_checking=true` means they're at 100% instantly). If you want hardlink-added torrents to remain paused, enable the "Skip auto-resume" option for your cross-seed source (Completion, RSS, Webhook, etc.).

### Notes

- Hardlinks share disk blocks with the original file but increase the link count. Deleting one link does not free space until all links to that file are removed.
- Windows: Folder names in the hardlink tree are sanitized to remove Windows-illegal characters (like `: * ? " < > |`). The torrent's internal file paths are not modified.
- Ensure the base directory has sufficient inode capacity for the number of hardlinks you expect to create.
- **Hardlink mode supports extra files when piece-boundary safe.** If the incoming torrent contains extra files not present in the matched torrent (e.g., `.nfo`/`.srt` sidecars), hardlink mode will link the content files and trigger a recheck so qBittorrent downloads the extras. If extras share pieces with content (unsafe), the cross-seed is skipped.

## Reflink Mode (optional)

Reflink mode creates copy-on-write clones of the matched files. Unlike hardlinks, reflinks allow qBittorrent to safely modify the cloned files (download missing pieces, repair corrupted data) without affecting the original seeded files.

**Key advantage:** Reflink mode **bypasses piece-boundary safety checks**. This means you can cross-seed torrents with extra/missing files even when those files share pieces with existing content—the clones can be safely modified.

### When to use

- You want to cross-seed torrents that hardlink mode would skip due to "extra files share pieces with content"
- Your filesystem supports copy-on-write clones (BTRFS, XFS on Linux; APFS on macOS)
- You prefer the safety of copy-on-write over hardlinks

### Requirements

- **Local filesystem access** must be enabled on the target qBittorrent instance.
- The base directory must be on the **same filesystem/volume** as the instance's download paths.
- The filesystem must support reflinks:
  - **Linux**: BTRFS, XFS (with reflink=1), and similar CoW filesystems
  - **macOS**: APFS
  - **Windows/FreeBSD**: Not currently supported

### Behavior differences from hardlink mode

| Aspect | Hardlink Mode | Reflink Mode |
|--------|--------------|--------------|
| Piece-boundary check | Skips if unsafe | Never skips (safe to modify clones) |
| Recheck | Only when extras exist | Always triggers recheck |
| Disk usage | Zero (shared blocks) | Starts near-zero; grows as modified |
| SkipRecheck option | Respects setting | Returns `skipped_recheck` if enabled |
| Below-threshold behavior | Auto-resume or pause | Always pauses for manual review |

### Disk usage implications

Reflinks use copy-on-write semantics:
- Initially, cloned files share disk blocks with originals (near-zero additional space)
- When qBittorrent writes to a clone (downloads extras, repairs pieces), only modified blocks are copied
- In worst case (entire file rewritten), disk usage approaches full file size
- If the original and clone become completely different, you're using 2x space

### How to enable

1. Enable "Local filesystem access" on the qBittorrent instance in Instance Settings.
2. In Cross-Seed > Hardlink / Reflink Mode, expand the instance you want to configure.
3. Enable "Reflink mode" for that instance.
4. Set "Base directory" to a path on the same filesystem as your downloads.
5. Choose a directory preset (`flat`, `by-tracker`, `by-instance`).

**Note:** Hardlink and reflink modes are mutually exclusive—only one can be enabled per instance.

### Recheck always required

Reflink mode always triggers a recheck after adding the torrent. This is because:
- The clone might have missing/extra files that need downloading
- qBittorrent must verify which pieces already match

If you have `SkipRecheck` enabled in your cross-seed settings, reflink mode will skip the cross-seed entirely rather than add without rechecking.

### Below-threshold behavior

If recheck completes below the configured threshold (default 95%):
- The torrent remains **paused for manual review**
- qui does NOT auto-delete the torrent or reflink directory
- This gives you a chance to investigate why completion is low

Check the torrent's progress and decide whether to:
- Resume it to download missing content
- Delete it if the match was incorrect

## Recheck & Alignment

### Default mode (reuse)

In default mode, qui points the cross-seed torrent at the matched torrent's existing files. If the incoming torrent has a different display name or folder structure, qui renames them to match. After rename-alignment, qBittorrent may need to recheck the torrent to verify files at the new paths.

Rechecks are also required when the source torrent contains extra files not present on disk (NFO, SRT, samples not filtered by ignore patterns).

**Auto-resume behavior:**
- Torrents that complete recheck at 95% or higher (configurable via "Size mismatch tolerance") auto-resume.
- Torrents below the threshold stay paused for manual investigation.

#### Piece-boundary safety for extra files

When the incoming torrent has extra files (files not present in the matched torrent), qBittorrent will download them during recheck. This is only safe when those extra files are **piece-boundary aligned**—meaning no torrent piece spans both existing content and the missing file.

**Why this matters:** BitTorrent pieces are hashed together. If a piece contains bytes from both your existing content file AND a missing file, qBittorrent would need to download new data that overlaps with your existing content—potentially corrupting it.

**What qui does:**
- Before adding any cross-seed with extra files, qui analyzes the torrent's piece layout
- If extra files share pieces with content files, the cross-seed is **skipped** with reason "extra files share pieces with content"
- If extra files are safely isolated (piece-boundary aligned), the cross-seed proceeds normally

This check applies regardless of whether the extra files match ignore patterns. The piece-boundary constraint is fundamental to how BitTorrent works.

### Hardlink mode

No rename-alignment is needed because the hardlink tree is created to match the incoming torrent's layout exactly.

When the incoming torrent has extra files not present in the matched torrent:
- qui hardlinks the content files that exist on disk
- The torrent is added paused, then qui triggers a recheck
- qBittorrent identifies the missing pieces (extras) and downloads them
- Once recheck completes at the configured threshold, qui auto-resumes the torrent

The same piece-boundary safety check applies: if extra files share pieces with content, the cross-seed is skipped to prevent data corruption.

## Category Behavior

### The .cross Suffix

When enabled, cross-seeded torrents get a `.cross` suffix on their category:
- Original torrent: `tv` category
- Cross-seed: `tv.cross` category

**Why?** This prevents *arr applications (Sonarr, Radarr, etc.) from seeing cross-seeded torrents, avoiding duplicate import attempts.

The `.cross` category is created automatically with the same save path as the base category.

### autoTMM (Auto Torrent Management)

Cross-seeds inherit the autoTMM setting from the matched torrent:
- If matched torrent uses autoTMM, cross-seed uses autoTMM
- If matched torrent has manual path, cross-seed uses same manual path

This ensures files are always saved to the correct location.

**Note:** When "Use indexer name as category" is enabled, autoTMM is always disabled for cross-seeds (explicit save paths are used instead).

## Save Path Determination

Priority order:
1. Base category's explicit save path (if configured in qBittorrent)
2. Matched torrent's current save path (fallback)

Example:
- `tv` category has save path `/data/tv`
- Cross-seed gets `tv.cross` category with save path `/data/tv`
- Files are found because they're in the same location

## Best Practices

**Do:**
- Use autoTMM consistently across your torrents
- Let qui create `.cross` categories automatically
- Keep category structures simple

**Don't:**
- Manually move torrent files after adding them
- Create `.cross` categories manually with different paths
- Mix autoTMM and manual paths for the same content type

## Troubleshooting

### Hardlink mode failed

Common causes:
- **Filesystem mismatch**: Hardlink base directory is on a different filesystem/volume than the download paths. Hardlinks cannot cross filesystems.
- **Missing local filesystem access**: The target instance doesn't have "Local filesystem access" enabled in Instance Settings.
- **Permissions**: qui cannot read the instance's content paths or write to the hardlink base directory.
- **Invalid base directory**: The hardlink base directory path doesn't exist and couldn't be created.

### "Files not found" after cross-seed (default mode)

This typically occurs in default mode when the save path doesn't match where files actually exist:
- Check that the cross-seed's save path matches where files actually exist
- Verify the matched torrent's save path in qBittorrent
- Ensure the matched torrent has completed downloading (100% progress)

### Reflink mode failed

Common causes:
- **Filesystem doesn't support reflinks**: The filesystem at the base directory doesn't support copy-on-write clones. On Linux, use BTRFS or XFS (with reflink enabled). On macOS, use APFS.
- **Filesystem mismatch**: Base directory is on a different filesystem than the download paths.
- **Missing local filesystem access**: The target instance doesn't have "Local filesystem access" enabled.
- **SkipRecheck enabled**: Reflink mode always requires recheck; if SkipRecheck is enabled, reflink mode skips the cross-seed.

### Cross-seed skipped: "extra files share pieces with content"

The incoming torrent has files not present in your matched torrent, and those files share torrent pieces with your existing content. Downloading them would require overwriting parts of your existing files.

**Solutions:**
- **Use reflink mode**: Enable reflink mode for the instance—it bypasses this check and safely clones files so qBittorrent can modify them
- This is expected for some torrents in hardlink/default mode—the skip protects your data
- If reflinks aren't available on your filesystem, you'd need to download the torrent fresh

### Cross-seed stuck at low percentage after recheck

- Check if the source torrent has extra files (NFO, samples) not present on disk
- Verify the "Size mismatch tolerance" setting in Global rules
- Torrents below the auto-resume threshold stay paused for manual review

### Cross-seed in wrong category

- Check your cross-seed settings in qui
- Verify the matched torrent has the expected category

### autoTMM unexpectedly enabled/disabled

- This mirrors the matched torrent's setting (intentional)
- Check the original torrent's autoTMM status in qBittorrent
- Note: "Use indexer name as category" always disables autoTMM
