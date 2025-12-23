# Cross-Seeding in qui

## How It Works

When you cross-seed a torrent, qui:
1. Finds a matching torrent in your library (same content, different tracker)
2. Adds the new torrent pointing to your existing files
3. Applies the correct category and save path automatically

qui supports two modes for handling files:

- **Default mode**: Reuses existing files directly. No new files or links are created. May require rename-alignment if the incoming torrent has a different folder/file layout.
- **Hardlink mode** (optional): Creates a hardlinked copy of the matched files laid out exactly as the incoming torrent expects, then adds the torrent pointing at that tree. Avoids rename-alignment entirely.

## Hardlink Mode (optional)

Hardlink mode is an opt-in strategy that creates a hardlinked file tree matching the incoming torrent's expected layout. The torrent is then added with `savepath` pointing to that tree and `skip_checking=true`, so qBittorrent can start seeding immediately without a hash recheck.

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

For `by-tracker` and `by-instance` presets, qui determines whether an isolation folder is needed based on the torrent's structure and qBittorrent's `torrent_content_layout` preference:

- **Subfolder layout**: qBittorrent always creates a root folder → no isolation folder needed
- **Original layout**: Root folder exists only if the torrent has a common top-level directory
  - Torrents with a root folder (e.g., `Movie/video.mkv`) → no isolation folder
  - Rootless torrents (e.g., `video.mkv`, `subs.srt`) → isolation folder added
- **NoSubfolder layout**: Root folder is stripped → isolation folder needed

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

## Recheck & Alignment

### Default mode

In default mode, qui points the cross-seed torrent at the matched torrent's existing files. If the incoming torrent has a different display name or folder structure, qui renames them to match. After rename-alignment, qBittorrent may need to recheck the torrent to verify files at the new paths.

Rechecks are also required when the source torrent contains extra files not present on disk (NFO, SRT, samples not filtered by ignore patterns).

**Auto-resume behavior:**
- Torrents that complete recheck at 95% or higher (configurable via "Size mismatch tolerance") auto-resume.
- Torrents below the threshold stay paused for manual investigation.

### Hardlink mode

No recheck or rename-alignment is needed because the hardlink tree matches the incoming layout.

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
