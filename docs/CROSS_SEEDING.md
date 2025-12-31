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
- The base directory must be a **real filesystem mount**, not a pooled/virtual mount (common examples: `mergerfs`, other FUSE mounts, `overlayfs`). Even if the underlying disks are XFS with `reflink=1`, reflink clone ioctls typically will not work through these layers.
- The filesystem must support reflinks:
  - **Linux**: BTRFS, XFS (with reflink=1), and similar CoW filesystems
  - **macOS**: APFS
  - **Windows/FreeBSD**: Not currently supported

Tip: On Linux, check the filesystem type for a path with `stat -f -c %T /path` (you want `xfs`/`btrfs`, not `fuseblk`/`fuse.mergerfs`/`overlayfs`). If qui runs in Docker, ensure you mount the **direct disk path** (e.g. `/mnt/disk1/...`) into the container.

### Behavior differences from hardlink mode

| Aspect | Hardlink Mode | Reflink Mode |
|--------|--------------|--------------|
| Piece-boundary check | Skips if unsafe | Never skips (safe to modify clones) |
| Recheck | Only when extras exist | Only when extras exist |
| Disk usage | Zero (shared blocks) | Starts near-zero; grows as modified |
| SkipRecheck option | Respects setting | Returns `skipped_recheck` only when recheck is required |
| Below-threshold behavior | Auto-resume or pause | Auto-resume or pause (when recheck runs) |

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

### When recheck is required

Recheck is required when the incoming torrent has **extra files** that are not present in the matched torrent. In this case qBittorrent needs a recheck to identify which pieces are missing so it can download the extras.

If there are no extra files, reflink mode adds the torrent with `skip_checking=true` (like hardlink mode) so it can start seeding immediately unless you enabled "Skip auto-resume".

If you have `SkipRecheck` enabled in your cross-seed settings, reflink mode will skip only the cases where a recheck is required (extra files).

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

Rechecks are also required when the source torrent contains extra files not present on disk (NFO, SRT, samples not matching allowed extra file patterns).

**Auto-resume behavior:**
- Torrents that complete recheck at 95% or higher (configurable via "Size mismatch tolerance") auto-resume.
- Torrents below the threshold stay paused for manual investigation.

#### Piece-boundary safety for extra files

When the incoming torrent has extra files (files not present in the matched torrent), qBittorrent will download them during recheck. This is only safe when those extra files are **piece-boundary aligned**—meaning no torrent piece spans both existing content and the missing file.

**Why this matters:** BitTorrent pieces are hashed together. If a piece contains bytes from both your existing content file AND a missing file, qBittorrent would need to download new data that overlaps with your existing content—potentially corrupting it.

**What qui does (when safety check is enabled):**
- Before adding any cross-seed with extra files, qui analyzes the torrent's piece layout
- If extra files share pieces with content files, the cross-seed is **skipped** with reason "extra files share pieces with content"
- If extra files are safely isolated (piece-boundary aligned), the cross-seed proceeds normally

This check applies regardless of whether the extra files match allowed extra file patterns. The piece-boundary constraint is fundamental to how BitTorrent works.

**Enabling this check:**
- **Piece boundary safety check** (opt-in): Uncheck "Skip piece boundary safety check" in Rules to enable this protection. When enabled, qui will skip matches where extra files share pieces with content.
- **Reflink mode** (always safe): Reflink mode never needs this check because copy-on-write clones can be safely modified without affecting originals.

**Why this check is opt-in:** Skipping the check maximizes cross-seed matches, especially for torrents with extra files (NFOs, samples, etc.) which are common. The tradeoff is potential data corruption if the matched content differs. Users who want extra protection can enable the check or use reflink mode.

### Hardlink mode

No rename-alignment is needed because the hardlink tree is created to match the incoming torrent's layout exactly.

When the incoming torrent has extra files not present in the matched torrent:
- qui hardlinks the content files that exist on disk
- The torrent is added paused, then qui triggers a recheck
- qBittorrent identifies the missing pieces (extras) and downloads them
- Once recheck completes at the configured threshold, qui auto-resumes the torrent

The piece-boundary safety check (if enabled) applies here too: if extra files share pieces with content, the cross-seed is skipped to prevent data corruption.

## Category Behavior

Cross-seeds can be assigned a category using one of three modes:

### 1. Category Suffix (default)

When enabled, cross-seeded torrents get a `.cross` suffix on their category:
- Original torrent: `tv` category
- Cross-seed: `tv.cross` category

**Why?** This prevents *arr applications (Sonarr, Radarr, etc.) from seeing cross-seeded torrents, avoiding duplicate import attempts.

The `.cross` category is created automatically with the same save path as the base category.

### 2. Indexer Name as Category

Uses the indexer name (from the search source) as the category. Useful for organizing cross-seeds by tracker.

### 3. Custom Category

Uses a user-specified static category name for all cross-seeds. No suffix is applied—the exact category name you enter is used.

### autoTMM (Auto Torrent Management)

autoTMM behavior depends on which category mode is active:

| Category Mode | autoTMM Behavior |
|---------------|------------------|
| **Suffix** (`.cross`) | Inherited from matched torrent |
| **Indexer name** | Always disabled (explicit save paths) |
| **Custom** | Always disabled (explicit save paths) |

When autoTMM is inherited (suffix mode):
- If matched torrent uses autoTMM, cross-seed uses autoTMM
- If matched torrent has manual path, cross-seed uses same manual path

When autoTMM is disabled (indexer/custom modes), cross-seeds always use explicit save paths derived from the matched torrent's location.

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
- **Pooled/virtual mount**: The base directory is on a pooled/virtual filesystem (like `mergerfs`, other FUSE mounts, or `overlayfs`) which often does not implement reflink cloning. Use a direct disk mount (e.g. `/mnt/disk1/...`) for both your seeded data and the reflink base directory.
- **Filesystem mismatch**: Base directory is on a different filesystem than the download paths.
- **Missing local filesystem access**: The target instance doesn't have "Local filesystem access" enabled.
- **SkipRecheck enabled**: If reflink mode would require recheck (extra files), it skips the cross-seed.

### Cross-seed skipped: "extra files share pieces with content"

This only occurs when you have enabled the piece boundary safety check (disabled "Skip piece boundary safety check" in Rules).

The incoming torrent has files not present in your matched torrent, and those files share torrent pieces with your existing content. Downloading them could overwrite parts of your existing files.

**Solutions:**
- **Use reflink mode** (recommended): Enable reflink mode for the instance—it safely clones files so qBittorrent can modify them without affecting originals
- **Disable the safety check**: Check "Skip piece boundary safety check" in Rules (the default). The match will proceed but **may corrupt your existing seeded files** if content differs
- If reflinks aren't available and you want to avoid any risk, download the torrent fresh

### Cross-seed stuck at low percentage after recheck

- Check if the source torrent has extra files (NFO, samples) not present on disk
- Verify the "Size mismatch tolerance" setting in Rules
- Torrents below the auto-resume threshold stay paused for manual review

### Cross-seed in wrong category

- Check your cross-seed settings in qui
- Verify the matched torrent has the expected category

### autoTMM unexpectedly enabled/disabled

- In suffix mode, autoTMM mirrors the matched torrent's setting (intentional)
- In indexer name or custom category mode, autoTMM is always disabled
- Check the original torrent's autoTMM status in qBittorrent

## ARR Integration (Sonarr/Radarr)

Configure Sonarr/Radarr instances in **Settings → Integrations** to enable external ID lookups (IMDb, TMDb, TVDb, TVMaze) during cross-seed searches. qui queries your *arr instances to resolve IDs, then includes them in Torznab requests for more accurate matching on indexers that support ID-based queries. Results are cached (30 days for found IDs, 1 hour for misses) to minimize API calls.
