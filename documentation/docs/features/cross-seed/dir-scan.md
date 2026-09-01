---
sidebar_position: 4
title: Directory Scanner
description: Scan local directories and automatically cross-seed completed downloads.
---

# Directory Scanner

Directory Scanner (Dir Scan) scans local folders to find cross-seed opportunities for content already on disk. Library Scan queries the qBittorrent torrent list. Dir Scan works directly with files on the filesystem.

Configure it in **Cross-Seed > Dir Scan**.

## Requirements

- Enable **Local filesystem access** in Instance Settings on at least one qBittorrent instance.
- qui must have direct read access to files on the same host or through shared mounts with the target qBittorrent instance.
- Configure at least one enabled Torznab indexer in **Settings > Indexers** (from Prowlarr, Jackett, or a native tracker endpoint).
- Optional: Configure Sonarr/Radarr in **Settings > Integrations** for external ID lookups (IMDb, TMDb, TVDb, TVMaze).

## How to Choose Your Scan Path

Dir Scan treats each **immediate child** of your configured path as one "searchee." It does not treat the path itself as a single searchee. It does not recurse into subfolders to create additional searchees.

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

Each immediate child (folder or file) becomes one searchee. Files within `Movie.2024.1080p.BluRay/` group together as part of that searchee.

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
| `/data/media/movies/Movie.2024.1080p.BluRay` | Only 1 searchee. Scans that specific movie only |

:::tip
Create one Dir Scan entry per category folder. Do not point at a parent folder that contains multiple category subfolders.
:::

## Docker and Path Mapping

If qui and qBittorrent run in separate containers or use different mount points, you must configure path mapping.

### "Local filesystem access" explained

When you enable **Local filesystem access** on a qBittorrent instance, it instructs qui:

1. qui can read files directly from the filesystem (same paths or mapped paths).
2. qui must use file-based matching (inode checks, size checks) instead of the qBittorrent API alone.

qui requires direct read access to the files, either on the same host or through shared network or volume mounts.

### Recommended: Use the same volume paths

The simplest configuration mounts volumes at the same path in both containers:

```yaml title="docker-compose.yml"
services:
  qui:
    volumes:
      - /mnt/storage:/mnt/storage

  qbittorrent:
    volumes:
      - /mnt/storage:/mnt/storage
```

If both containers see `/data/media/movies`, you do not need path mapping. Leave **qBittorrent Path Prefix** empty.

### Path mapping example (different mount points)

Container mounts:

- qui container mounts: `-v /mnt/storage:/data`
- qBittorrent container mounts: `-v /mnt/storage:/downloads`

qui sees files at `/data/media/movies/Movie.2024/movie.mkv`.
qBittorrent sees the same file at `/downloads/media/movies/Movie.2024/movie.mkv`.

Configure Dir Scan:

- **Directory Path**: `/data/media/movies`
- **qBittorrent Path Prefix**: `/downloads/media/movies`

When qui finds a match, it tells qBittorrent to add the torrent at `/downloads/media/movies/Movie.2024/` instead of `/data/media/movies/Movie.2024/`.

## How It Works

For each configured scan directory, qui:

1. Enumerates immediate children of the directory path.
2. Recursively collects all files within each child (folder or file).
3. Groups files into a "searchee" with parsed release info.
4. Uses configured *arr instances to resolve external IDs when possible.
5. Searches enabled indexers via Torznab.
6. Downloads torrent files and matches their file lists against the files on disk.
7. If it finds a match, adds the torrent to the target qBittorrent instance.

:::note Categories + AutoTMM
Dir Scan adds torrents with an explicit `savepath` to point qBittorrent at the existing files on disk. That forces **AutoTMM off** for Dir Scan injections.

Dir Scan categories come only from **Dir Scan → Default Category** and per-directory **Category override**. Cross-Seed → Rules category modes (affix / indexer / custom) do not apply to Dir Scan.

If you later enable AutoTMM on an injected torrent, qBittorrent can relocate files based on its default save path and category rules.
:::

:::info
Torznab searches run through the shared scheduler at background priority. They queue behind interactive, RSS, and completion cross-seed work.

If the global scan concurrency limit is reached, new scans show as `queued` until a scan slot opens.
Dir Scan also pauses between candidate torrent-file downloads from an indexer. This protects Prowlarr and indexers from request bursts, which matters most on private trackers. This delay increases scan times when qui checks many candidates.
:::

### Already-seeding detection

Dir Scan maintains a FileID index (inode + device on Unix) to track files present in qBittorrent. It skips:

- Files that are already part of a seeding torrent
- Torrents whose infohash already exists in qBittorrent

This prevents redundant searches and duplicate additions.

If you remove a torrent from qBittorrent, its files leave the index. For example, an automation rule can remove torrents with missing files. The next scan of the directory treats those files as new searchees and searches indexers for them again.

### Recheck Behavior

- **Full matches**: qui adds the torrent with "skip hash check" enabled. Seeding starts immediately unless **Start torrents paused** is enabled.
- **Partial matches** (when enabled): qui adds the torrent without a skipped hash check. qBittorrent verifies existing data and downloads missing files.

## What Gets Scanned

### Included file types

**Video:** `.mkv`, `.mp4`, `.avi`, `.m4v`, `.wmv`, `.mov`, `.ts`, `.m2ts`, `.vob`, `.mpg`, `.mpeg`, `.webm`, `.flv`

**Audio:** `.flac`, `.mp3`, `.wav`, `.aac`, `.ogg`, `.m4a`, `.wma`, `.ape`, `.alac`, `.dsd`, `.dsf`, `.dff`

**Extras:** `.nfo`, `.sfv`, `.srt`, `.sub`, `.idx`, `.ass`, `.ssa`

Extras belong to releases and affect partial-match behavior. If a torrent includes an `.nfo` file that does not exist on disk, the result is a partial match, not a full match.

### Disc layouts

qui treats folders that contain `BDMV/`, `VIDEO_TS/`, or `AUDIO_TS/` structures as disc-based media. It includes all files within these structures, regardless of extension.

### Skipped items

- **Hidden files and folders** (names starting with `.`)
- **Symlinks inside a searchee folder** (skipped to avoid loops and permission issues). qui follows a symlinked media file placed directly in the scan root and scans its target. qui does not enter a symlinked folder in the scan root.
- **Files with permission errors** (qui continues the scan and skips the file)
- **Non-media files** outside disc layouts

## Settings (Global)

Open **Dir Scan > Settings**:

| Setting | Description |
|---------|-------------|
| Match Mode | `Strict` matches by filename + exact file size. `Flexible` ignores filenames for primary matching, but matched files must still have the same exact file size. |
| Size Tolerance (%) | Allows small differences in total torrent size when qui filters candidates before file matching. |
| Minimum Piece Ratio (%) | Sets the minimum percentage of torrent data that must exist on disk for partial matches. |
| Max searchees per run | Limits how many eligible searchees each run processes. Set `0` for unlimited. Useful for progress across restarts. |
| Only process items changed within the last (days) | Excludes stale work items before search. Uses video/audio mtimes only for manual and scheduled scans. Webhook scans ignore this cutoff. Set `0` to disable. |
| Allow partial matches | Adds torrents even if they have extra or missing files compared to disk. |
| Download missing files | Downloads files missing from disk for partial matches. Hardlink and reflink modes require this setting for season packs and partial releases. Enabled by default. |
| Skip piece boundary safety check | Allows partial matches where missing-file downloads can modify pieces that contain existing content. |
| Start torrents paused | Adds injected torrents in a paused state. |
| Default Category / Tags | Applies categories and tags to all injected torrents. Directory settings add to these values. |

In practice:

- **Strict** works best when filenames on disk match the release layout.
- **Flexible** works best for renamed libraries, but it still requires exact file-size matches for paired files.
- **Size Tolerance** affects only candidate filtering based on **total torrent size**. It does **not** allow per-file size mismatches.
- If a candidate lacks corroborating title or external ID evidence, qui rejects flexible single-file matches. This prevents false positives when an indexer falls back from ID-based search to plain title search.

### "Max searchees per run" explained

This setting limits how many **top-level folders/files** Dir Scan processes in a single run.

- If your directory is a TV root like `/mnt/storage/media/tv`, each **show folder** is one searchee (for example `Show.Name/`, `Another.Show/`).
- If your directory is a movies root like `/mnt/storage/media/movies`, each **movie folder** is one searchee (for example `Movie.Title (2024)/`, `Another.Movie (2023)/`).

If you configure **Max searchees per run = 5**, Dir Scan processes up to **5 show folders** (TV) or **5 movie folders** (movies) per run. It stops and saves per-file progress for the next run. The next run rechecks the directory, skips finalized files, and retries unfinished work. See [Incremental progress and resets](#incremental-progress-and-resets).

This setting does **not** cap the total number of indexer searches. TV folders can trigger multiple searches (season-level and per-episode heuristics), but count as a single top-level searchee.

### "Only process items changed within the last (days)" explained

This setting reduces tracker and API load. It excludes stale content before searches start.

- If you scan movies or music, items stay in scope only when their newest video or audio file falls within the cutoff.
- For TV, qui evaluates work items at the season and episode level. A single fresh episode does not pull an entire older show into scope.
- If all episode files in a season work item fall within the cutoff, qui keeps the season-pack search. Otherwise, qui falls back to fresh episode-level work only. If you enable [Skip individual episodes](#skip-individual-episodes), no episode-level fallback occurs. The season pack stays in scope as long as its newest episode falls within the cutoff.
- qui computes the cutoff as `now - N days` (for example, `7` means "older than 7 days").
- The timestamp is the filesystem **modified time (mtime)** from video and audio files only. It ignores subtitle and extra mtimes, release dates, and qBittorrent add times.
- Webhook scans ignore the cutoff and use the webhook path as the freshness signal.
- `0` disables age filtering.

Example with `7` days:

- If `Movie.2024/` has only an `.srt` updated yesterday while the `.mkv` is old, qui skips the folder.
- If `Show.Name/Season 01/` has one fresh episode and nine old ones, only the fresh episode stays in scope.
- If `Old.Show.S01/` has all episode files older than 7 days, qui skips the folder.

## Directories

Each scan directory has its own configuration:

| Setting | Description |
|---------|-------------|
| Directory Path | The path qui scans (immediate children become searchees). |
| qBittorrent Path Prefix | Path mapping for container configurations. See [Docker and Path Mapping](#docker-and-path-mapping). |
| Target qBittorrent Instance | Where qui adds matched torrents. Must have Local filesystem access enabled. |
| Category override | Overrides the global Default Category for this directory. |
| Additional tags | Added on top of the global Dir Scan tags. |
| Scan Interval (minutes) | How often qui rescans (minimum 60 minutes, default 1440 = 24 hours). |
| Skip individual episodes | The scan does not search single TV episodes. See [Skip individual episodes](#skip-individual-episodes). |
| Enabled | Enable or disable the directory without deleting the configuration. |

### Skip individual episodes

This option is off by default. When enabled, Dir Scan does not search for single TV episodes. It searches for a season pack containing the episodes instead. This setting does not affect movies, music, or other content.

A season pack search requires two or more episodes of the same show and season. Dir Scan groups these episodes across the entire searchee, not per subfolder. If a folder contains episodes from multiple seasons, Dir Scan creates no pack for those seasons. Dir Scan skips any episode that cannot form a season pack.

This option does not affect specials (season 0) or absolute episode numbers.

## Operational Behavior

### Concurrent scans

Only one scan runs per directory at a time. If a scheduled scan triggers while another scan runs, qui does not start a second run for that directory.

### Incremental progress and resets

Dir Scan persists per-file progress. It skips unchanged searchees whose files are already in a final state (matched, no match, already seeding, or in qBittorrent).

This is **not** an exact checkpoint resume. If you start a new run after a cancellation or qui restart, Dir Scan:

- Rechecks the directory from the top.
- Skips unchanged finished files.
- Retries unfinished or failed files.

This behavior functions as a **restart with preserved progress** rather than continuing from the exact stoppage point.

### New indexers reopen "no match" files

Each "no match" file records which indexers were enabled when the search ran. If you enable an indexer missing from that record, the next scan searches the file again. You do not need to reset anything. An indexer present in the record does not trigger a retry, because qui already searched it.

Dir Scan marks a file as "no match" only when every queried indexer responds. If any indexers are down or rate-limited, the file remains pending and the next scan retries it.

Files marked "no match" on older qui versions lack a recorded indexer set. They remain skipped until you requeue them.

### Retry Unmatched vs Reset Scan Progress

Both buttons sit on the directory details card, next to the run history.

- **Retry Unmatched** resets only "no match" files to pending. Matched and already-seeding files keep their state. Use this button after you add an indexer to search old "no match" files again.
- **Reset Scan Progress** deletes all tracked file state for the directory. The next scan reprocesses all files, including previous matches. Use this button only for a complete reset.

Neither button starts a scan. Trigger a scan with **Scan Now**, or wait for the next scheduled run.

### Scheduled vs manual scans

- **Scheduled scans** run at the configured interval (minimum 60 minutes).
- **Manual scans** start from the UI at any time with the **Scan Now** button.

You can cancel both types from the UI while they run.

The UI displays the **last 10 run entries** per directory. qui prunes older run rows automatically.

### Webhook trigger

You can trigger a scan automatically when Sonarr, Radarr, Lidarr, or Readarr imports content. The webhook endpoint natively understands *arr webhook payloads without custom scripts.

```http
POST /api/dir-scan/webhook/scan?apikey=YOUR_API_KEY
```

qui extracts the path from the *arr payload (`series.path`, `movie.folderPath`, `artist.path`, or `author.path`). It matches the path against the Dir Scan **Directory Path** values configured in qui and uses the payload path as the scan root. It does not use qBittorrent path prefixes for this lookup. On success, the response includes `runId`, `directoryId`, `directoryPath`, and `scanRoot`.

Each Dir Scan directory can also define **Allowed Download Clients**. If configured, native *arr webhook scans run only when the payload `downloadClient` matches one of those names. Leave the list empty to accept all clients. Matching is case-insensitive and trims surrounding whitespace. Direct simple-mode `{"path": ...}` callers bypass the download client filter.

#### Configuration in Sonarr / Radarr

1. Go to **Settings → Connect → Add → Webhook**.
2. Set **Name** to a value like `qui Dir Scan`.
3. Under **Notification Triggers**, enable **On File Import**. If you want scans after upgrades, enable **On File Upgrade**. In Sonarr, **On Import Complete** also works.
4. Set **Webhook URL** to:
   ```text
   http://your-qui-host:7476/api/dir-scan/webhook/scan?apikey=YOUR_API_KEY
   ```
5. Set **Method** to `POST`.
6. Leave **Username** and **Password** empty. The API key in the URL handles authentication.
7. Click **Test** or **Save**. qui accepts the built-in **Test** action as a no-op health check and does not start a scan.

The same steps apply to Lidarr and Readarr.

:::tip
The webhook uses query-param API key authentication (`?apikey=...`), matching the cross-seed webhook format. You can also use the `X-API-Key` header instead.
:::

#### How path matching works

qui uses longest-prefix matching against the configured Dir Scan **Directory Path** values to choose which directory settings apply. The actual scan root is the path from the webhook payload. For example, you configure directories for `/data/media/movies` and `/data/media/tv`. If Sonarr sends `series.path: "/data/media/tv/Show Name"`, qui matches `/data/media/tv` and scans `/data/media/tv/Show Name`.

In split-mount configurations, the *arr app must send the same library path that qui sees on disk. If Sonarr or Radarr uses a different mount path than qui, the webhook does not find a matching directory.

#### Response codes

| Status Code | Meaning |
|-------------|---------|
| `200` | Webhook accepted but skipped by directory filters. Example: `{"skipped": true, "reason": "download client not allowed"}` |
| `202` | Scan accepted. If the directory is idle, qui starts the run immediately. If a webhook scan already runs for that directory, qui keeps one follow-up queued run and merges later webhook paths into it. Example: `{"runId": 42, "directoryId": 3, "directoryPath": "/data/media/tv", "scanRoot": "/data/media/tv/Show Name"}` |
| `204` | Test webhook accepted. No scan started |
| `400` | Invalid JSON payload, or no supported path field in the request body |
| `404` | No enabled directory matches the path in the payload |
| `409` | Request conflicts with directory state, such as multiple matching directories |
| `500` | Internal server error. qui failed to start the scan because of an internal failure |

If a second webhook arrives while the same directory is already scanning, qui returns `202` again. It does not reject the request and does not require client-side retries. Instead, it updates one queued follow-up run for that directory and expands the queued `scanRoot` to the nearest common ancestor when needed.

Webhook-triggered scans also ignore the global age cutoff. This avoids false skips when Sonarr/Radarr imports files that keep old filesystem mtimes.

#### Allowed download clients

Use **Allowed Download Clients** on a Dir Scan directory when you want only specific Sonarr/Radarr clients to trigger scans for that path.

- Leave it empty to allow all webhook clients.
- Add exact client names as shown in Sonarr or Radarr, such as `SABnzbd`, `NZBGet`, or `qBittorrent`.
- Matching is case-insensitive and ignores surrounding whitespace.
- If the webhook is otherwise valid but the client is missing or not allowed, qui returns `200` and does not start a scan.
- Direct simple-mode callers with `{"path": ...}` bypass this filter because they provide no *arr download client metadata.

#### Simple mode

You can also call the webhook directly with a plain path. This is useful for scripts and other tools:

```bash
curl -X POST "http://localhost:7476/api/dir-scan/webhook/scan?apikey=YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{"path": "/data/media/movies/Movie Name (2024)"}'
```

### Scan phases

Each scan moves through these phases:

1. **Scanning**: qui reads the directory contents and builds the searchee list
2. **Searching**: qui queries indexers for each searchee
3. **Injecting**: qui adds matched torrents to qBittorrent
4. **Final state**: Success, Failed, or Canceled

The UI shows the current phase and progress during active scans.

## Hardlink/Reflink Modes

You configure each instance under **Cross-Seed > Rules > [Hardlink / Reflink Mode](./hardlink-mode.md#how-to-enable)**.

If the target qBittorrent instance has hardlink or reflink mode enabled, Dir Scan uses the same behavior as other cross-seed methods:

- Builds a link tree that matches the incoming torrent's layout.
- Adds the torrent pointing at that tree (`contentLayout=Original`). Full matches use `skip_checking=true`. For partial matches, qBittorrent verifies existing data and downloads missing files into the link tree.

:::note
Partial matches in link tree mode (hardlink or reflink) require **Download missing files** in Dir Scan settings. Without it, qui rejects partial link tree injections.
:::

See:

- [Hardlink Mode](./hardlink-mode.md)
- [Link Directories](./link-directories.md)

### Fallback to regular mode

If link-tree creation fails (such as across filesystems or on permission errors) and the instance enables **Fallback to regular mode**, Dir Scan falls back to regular add behavior. Otherwise, the candidate fails.

Filesystem fallback adds the torrent against the matched source files instead of the link-tree directory. If the torrent file names differ from the on-disk names, qui adds the torrent paused, renames the paths, rechecks it, and resumes it. Partial matches and disc layouts get a recheck after the add and resume when the check completes. The cross-seed **Skip recheck** rule does not apply to Dir Scan.

Dir Scan runs the piece-boundary check once, before it adds a partial match. If you enable **Skip piece boundary safety check**, Dir Scan skips the check for link-tree adds and fallback adds alike.

## Scanning Your *arr Library

Dir Scan can scan Sonarr/Radarr library folders. Use caution with partial matches:

:::warning
If you enable **Allow partial matches**, qBittorrent can download missing files (extras like `.nfo`, subtitles) directly into your *arr-managed library folder. This creates unexpected files next to your media.
:::

For a "read-only" scan of your library:

1. Disable **Allow partial matches** (full matches only).
2. Disable **Fallback to regular mode** on the target instance. Hardlink failures then do not add torrents directly against your library path.

The safer configuration is usually:

- Scan your completed downloads or staging folder instead of the final library.
- Use hardlink or reflink mode, so cross-seeds live under your configured link-tree base directory.

## Troubleshooting

### Recent Scan Runs

The **Recent Scan Runs** panel on the Dir Scan page shows:

- Start time, status, file count, match count, added count, and duration
- An error icon next to the status of a failed run

Expand a run to see each added or failed torrent and its failure reason.

### Common issues

**No results found:**

- Make sure that at least one indexer is enabled and not rate-limited.
- Make sure that the scan path contains valid media files.
- Make sure that the target instance has **Local filesystem access** enabled.

**Permissions errors:**

- qui must have read access to the scan path.
- If you run in Docker, check container volume mounts.

**Wrong path mapping:**

- Make sure that **qBittorrent Path Prefix** matches how qBittorrent sees the same files.
- Test with a torrent's save path in the qBittorrent UI.

**Rate limiting:**

- Indexers can throttle requests. Check **Scheduler Activity** on the Indexers page.
- Reduce the scan frequency or limit the scan to fewer indexers.

For cross-seed-wide issues (matching behavior, hardlink failures, recheck problems), see [Troubleshooting](./troubleshooting.md).
