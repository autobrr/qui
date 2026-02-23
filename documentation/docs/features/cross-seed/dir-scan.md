---
sidebar_position: 4
title: Directory Scanner
description: Scan local directories and automatically cross-seed completed downloads.
---

# Directory Scanner

Directory Scanner (Dir Scan) scans local folders to find cross-seed opportunities for content already on disk. Unlike Library Scan (which queries qBittorrent's torrent list), Dir Scan works directly with files on the filesystem.

Configure it in **Cross-Seed > Dir Scan**.

## Requirements

- At least one qBittorrent instance must have **Local filesystem access** enabled in Instance Settings.
- qui must be able to read the files directly (same host or shared mounts as the target qBittorrent instance).
- Prowlarr or Jackett must be configured with at least one enabled indexer.
- Optional: Sonarr/Radarr configured in **Settings > Integrations** for external ID lookups (IMDb/TMDb/TVDb).

## How to Choose Your Scan Path

Dir Scan treats each **immediate child** of your configured path as one "searchee." It does not treat the path itself as a single searchee, and it does not recurse into subfolders to create additional searchees.

**Example:** If you configure `/data/media/movies`:

```plaintext
/data/media/movies/
├── Movie.2024.1080p.BluRay/   <- searchee 1
│   ├── movie.mkv
│   └── movie.nfo
├── Another.Movie.2023.2160p/  <- searchee 2
│   └── movie.mkv
└── standalone.mkv             <- searchee 3
```

Each immediate child (folder or file) becomes one searchee. Files within `Movie.2024.1080p.BluRay/` are grouped together as part of that searchee.

### Correct path choices

| Content type | Recommended path | Why |
|-------------|------------------|-----|
| Movies | `/data/media/movies` | Each movie folder is one searchee |
| TV Shows | `/data/media/tv` | Each show folder is one searchee |
| Music | `/data/media/music` | Each album folder is one searchee |

### Incorrect path choices

| Path | Problem |
|------|---------|
| `/data/media` containing `movies/` + `tv/` + `music/` | Only 3 searchees total (the category folders themselves) |
| `/data/media/movies/Movie.2024.1080p.BluRay` | Only 1 searchee; scans that specific movie only |

:::tip
Create one Dir Scan entry per category folder. Don't point at a parent folder containing multiple category subfolders.
:::

## Docker and Path Mapping

When qui and qBittorrent run in separate containers or see different mount points, you need path mapping.

### "Local filesystem access" explained

Enabling **Local filesystem access** on a qBittorrent instance tells qui:
1. qui can read files directly from the filesystem (same paths or mapped paths).
2. qui should use file-based matching (inode checks, size verification) rather than relying solely on qBittorrent's API.

This requires qui to have read access to the actual files, either on the same host or via shared network/volume mounts.

### Recommended: Use the same volume paths

The simplest setup is to mount volumes at the same path in both containers:

```yaml title="docker-compose.yml"
services:
  qui:
    volumes:
      - /mnt/storage:/mnt/storage

  qbittorrent:
    volumes:
      - /mnt/storage:/mnt/storage
```

When both containers see `/data/media/movies`, no path mapping is needed. Leave **qBittorrent Path Prefix** empty.

### Path mapping example (different mount points)

Your setup:
- qui container mounts: `-v /mnt/storage:/data`
- qBittorrent container mounts: `-v /mnt/storage:/downloads`

qui sees files at `/data/media/movies/Movie.2024/movie.mkv`
qBittorrent sees the same file at `/downloads/media/movies/Movie.2024/movie.mkv`

Configure Dir Scan:
- **Directory Path**: `/data/media/movies`
- **qBittorrent Path Prefix**: `/downloads/media/movies`

When qui finds a match, it tells qBittorrent to add the torrent pointing at `/downloads/media/movies/Movie.2024/` instead of `/data/media/movies/Movie.2024/`.

## How It Works

For each configured scan directory, qui:

1. Enumerates immediate children of the directory path.
2. For each child (folder or file), recursively collects all files within.
3. Groups files into a "searchee" with parsed release info.
4. Uses configured *arr instances to resolve external IDs when possible.
5. Searches enabled indexers via Torznab.
6. Downloads torrent files and matches their file lists against what's on disk.
7. If a match is found, adds the torrent to the target qBittorrent instance.

:::info
Torznab searches run through the shared scheduler at background priority, so they queue behind interactive, RSS, and completion cross-seed work.

If the global scan concurrency limit is reached, new scans show as `queued` until a scan slot is available.
Dir Scan may also pause between downloading candidate torrent files from an indexer. This is intentional and helps avoid hammering Prowlarr/indexers (especially for private trackers), but it can make scans take longer when many candidates need checking.
:::

### Already-seeding detection

Dir Scan maintains a FileID index (inode + device on Unix) to track files already present in qBittorrent. It skips:
- Files that are already part of a seeding torrent
- Torrents whose infohash already exists in qBittorrent

This avoids redundant searches and duplicate additions.

### Recheck Behavior

- **Full matches**: Torrent is added with "skip hash check" enabled. Seeding starts immediately.
- **Partial matches** (when enabled): Torrent is added without skipping hash check. qBittorrent verifies existing data and downloads missing files.

## What Gets Scanned

### Included file types

**Video:** `.mkv`, `.mp4`, `.avi`, `.m4v`, `.wmv`, `.mov`, `.ts`, `.m2ts`, `.vob`, `.mpg`, `.mpeg`, `.webm`, `.flv`

**Audio:** `.flac`, `.mp3`, `.wav`, `.aac`, `.ogg`, `.m4a`, `.wma`, `.ape`, `.alac`, `.dsd`, `.dsf`, `.dff`

**Extras:** `.nfo`, `.sfv`, `.srt`, `.sub`, `.idx`, `.ass`, `.ssa`

Extras are included in releases and can affect partial-match behavior (a torrent with an `.nfo` you don't have may trigger a partial match instead of full).

### Disc layouts

Folders containing `BDMV/`, `VIDEO_TS/`, or `AUDIO_TS/` structures are treated as disc-based media. All files within these structures are included regardless of extension.

### Skipped items

- **Hidden files and folders** (names starting with `.`)
- **Symlinks** (explicitly skipped to avoid loops and permission issues)
- **Files with permission errors** (scan continues, file is skipped)
- **Non-media files** outside disc layouts

## Settings (Global)

Open **Dir Scan > Settings**:

| Setting | Description |
|---------|-------------|
| Match Mode | `Strict` matches by filename + size. `Flexible` matches by size only. |
| Size Tolerance (%) | Allows small size differences when matching. |
| Minimum Piece Ratio (%) | For partial matches, minimum percent of torrent data that must exist on disk. |
| Max searchees per run | Limits how many eligible searchees are processed per run. `0` = unlimited. Useful for making progress across restarts. |
| Skip searchees older than (days) | Excludes searchees where all files are older than the cutoff. `0` = disabled. |
| Allow partial matches | Add torrents even if they have extra/missing files compared to disk. |
| Skip piece boundary safety check | Allow partial matches where downloading missing files could modify pieces containing existing content. |
| Start torrents paused | Add injected torrents in paused state. |
| Default Category / Tags | Applied to all injected torrents. Directory-level settings add to these. |

### "Max searchees per run" explained

This setting limits how many **top-level folders/files** Dir Scan will process in a single run.

- If your directory is a TV root like `/mnt/storage/media/tv`, then each **show folder** is one searchee (for example `Show.Name/`, `Another.Show/`).
- If your directory is a movies root like `/mnt/storage/media/movies`, then each **movie folder** is one searchee (for example `Movie.Title (2024)/`, `Another.Movie (2023)/`).

So if **Max searchees per run = 5**, Dir Scan will process up to **5 show folders** (TV) or **5 movie folders** (movies) per run, then stop and persist per-file progress for the next run (so already-final files won't be reprocessed). See [Incremental progress and resets](#incremental-progress-and-resets).

This is **not** a cap on the total number of indexer searches. TV folders can trigger multiple searches (season-level + per-episode heuristics), even though they still count as a single top-level searchee.

### "Skip searchees older than (days)" explained

This setting reduces tracker/API load by excluding stale content before search begins.

- A searchee is excluded only if **all files in that searchee** are older than the cutoff.
- Cutoff is computed as `now - N days` (for example, `7` means “older than 7 days”).
- The timestamp used is filesystem **modified time (mtime)**, not release date or qBittorrent add time.
- `0` disables age filtering.

Example with `7` days:

- `Movie.2024/` has one subtitle updated yesterday -> included.
- `Old.Show.S01/` has all files older than 7 days -> skipped.

## Directories

Each scan directory has its own configuration:

| Setting | Description |
|---------|-------------|
| Directory Path | The path qui scans (immediate children become searchees). |
| qBittorrent Path Prefix | Path mapping for container setups. See [Docker and Path Mapping](#docker-and-path-mapping). |
| Target qBittorrent Instance | Where matched torrents are added. Must have Local filesystem access enabled. |
| Category override | Overrides the global Default Category for this directory. |
| Additional tags | Added on top of the global Dir Scan tags. |
| Scan Interval (minutes) | How often to rescan (minimum 60 minutes, default 1440 = 24 hours). |
| Enabled | Enable/disable without deleting the configuration. |

## Operational Behavior

### Concurrent scans

Only one scan runs per directory at a time. If a scheduled scan triggers while another scan is running, it will not start a second run for that directory.

### Incremental progress and resets

Dir Scan persists per-file progress and skips unchanged searchees whose files are already in a final state (matched/no match/already seeding/in qBittorrent). This makes scans resumable across restarts.

If you want to force a directory to be re-processed from scratch, use **Reset Scan Progress** for that directory in the UI. This clears the tracked file state for that directory.

### Scheduled vs manual scans

- **Scheduled scans** run based on the configured interval (minimum 60 minutes).
- **Manual scans** can be triggered from the UI at any time via the "Scan Now" button.

Both types can be canceled from the UI while running.

### Scan phases

Each scan progresses through phases:

1. **Scanning** - Reading directory contents and building searchee list
2. **Searching** - Querying indexers for each searchee
3. **Injecting** - Adding matched torrents to qBittorrent
4. **Final state** - Success, Failed, or Canceled

The UI shows current phase and progress during active scans.

## Hardlink/Reflink Modes

If the target qBittorrent instance has hardlink or reflink mode enabled, Dir Scan uses the same behavior as other cross-seed methods:

- Builds a link tree matching the incoming torrent's layout.
- Adds the torrent pointing at that tree (`contentLayout=Original`). Full matches use `skip_checking=true`; partial matches allow qBittorrent to verify existing data and download missing files safely into the link tree.

See:
- [Hardlink Mode](hardlink-mode)
- [Link Directories](link-directories)

### Fallback to regular mode

When link-tree creation fails (hardlinking across filesystems, permission issues), Dir Scan falls back to regular add behavior **if** the instance has **Fallback to regular mode** enabled. Otherwise, the candidate fails.

## Scanning Your *arr Library

Dir Scan can scan Sonarr/Radarr library folders, but be careful with partial matches:

:::warning
With **Allow partial matches** enabled, qBittorrent may download missing files (extras like `.nfo`, subtitles) directly into your *arr-managed library folder. This can create unexpected files alongside your media.
:::

For a "read-only" scan of your library:
1. Disable **Allow partial matches** (full matches only).
2. Disable **Fallback to regular mode** on the target instance so hardlink failures don't add torrents directly against your library path.

The safer setup is usually:
- Scan your completed downloads/staging folder instead of the final library, and/or
- Use hardlink/reflink mode so cross-seeds live under your configured link-tree base directory.

## Troubleshooting

### Recent Scan Runs

The **Recent Scan Runs** panel on the Dir Scan page shows:
- Added count (successful injections)
- Failed count (matches that couldn't be added)
- Timestamps and duration

Click a run to see details including failure reasons for individual items.

### Common issues

**No results found:**
- Verify at least one indexer is enabled and not rate-limited.
- Check that the scan path contains valid media files.
- Ensure the target instance has Local filesystem access enabled.

**Permissions errors:**
- qui must have read access to the scan path.
- Check container volume mounts if running in Docker.

**Wrong path mapping:**
- Verify qBittorrent Path Prefix matches how qBittorrent sees the same files.
- Test by checking a torrent's save path in qBittorrent's UI.

**Rate limiting:**
- Indexers may throttle requests. Check **Scheduler Activity** on the Indexers page.
- Consider reducing scan frequency or limiting to fewer indexers.

For cross-seed-wide issues (matching behavior, hardlink failures, recheck problems), see [Troubleshooting](troubleshooting).
