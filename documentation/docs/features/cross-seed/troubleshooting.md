---
sidebar_position: 8
title: Troubleshooting
description: Fix common cross-seed problems.
---

# Cross-Seed Troubleshooting

## Why was my cross-seed not added?

### Rate limiting (HTTP 429)

Indexers limit request frequency. If you see an error such as `indexer TorrentLeech query rate-limited until ...`, qui uses the Retry-After value from the response, or 1 minute if the indexer sends none. It skips that indexer until the cooldown ends. qui tracks search (`query`) and download (`grab`) cooldowns separately. The **Scheduler Activity** panel on the Indexers page shows which indexers are in cooldown and when they become ready.

### Release didn't match

qui uses strict matching to ensure that cross-seeds have identical files. Both releases must match on:
- Title, year, and release group
- Resolution (1080p, 2160p)
- Source (WEB-DL, BluRay) and collection (AMZN, NF)
- Codec (x264, x265) and HDR format
- Audio format and channels
- Language, edition, cut, and version (v2, v3)
- Variants like IMAX, HYBRID, REPACK, PROPER

If strict release matching rejects an approved metadata difference, some discovery methods use an exact reported byte count.

Interactive Torznab search, Library Scan, and completion search share this rule. RSS uses the title and byte count from the feed before its single download.

The autobrr `/check` endpoint uses `.Size` without fetching the torrent file. The value is exact, rounded, or `0`.

If autobrr sends `0`, qui uses a narrow name-only preflight. The `/apply` endpoint calculates the actual size and repeats the match against live sources.

An exact reported size is evidence, not byte verification. qui still checks the downloaded torrent metadata, files, layout, and piece boundaries.

### Season pack vs episodes

By default, season packs only match other season packs. If you enable **Cross-seed episodes from packs** under Cross-Seed > Rules, season packs match individual episode releases.

### Prowlarr filters remove expected results

qui searches the selected Prowlarr indexer directly. If that indexer has a search filter enabled, such as freeleech only, the filter also applies to cross-seed searches. The tracker can return no results even when matching cross-seed candidates exist.

Prowlarr does not support per-search filter overrides. Add a second entry for the tracker in Prowlarr with the filter disabled, then select that second entry in qui.

## Cross-seed search run statuses

Library scan and completion search rows use **added**, **skipped**, or **failed** as the top-level outcome. Open the row details to see the per-attempt status and message.

| Status or message | Outcome | What it usually means | What to check |
| --- | --- | --- | --- |
| `exists` | Skipped | The exact torrent infohash is already in the target qBittorrent instance. | This is normally harmless. If you expected a new tracker result, check the source and target indexers in [Cross-Seed Overview](./overview.md#discovery-methods). |
| `no_match` | Skipped | qui searched but did not find an existing local torrent with the required files. | Review [release matching](#release-didnt-match), source filters, and the discovery method in [Library Scan](./overview.md#library-scan) or [Auto-Search on Completion](./overview.md#auto-search-on-completion). |
| `blocked` | Skipped | The candidate infohash is on the cross-seed blocklist. | If you want qui to try it again, remove it from **Cross-Seed > Blocklist**. See [Blocklist](./overview.md#blocklist). |
| `skipped_recheck` | Skipped | The match requires a recheck, but **Skip recheck** is enabled. | See [When Rechecks Are Required](#when-rechecks-are-required-reuse-mode) and [Rules](./rules.md#matching). |
| `skipped_unsafe_pieces` | Skipped | The incoming torrent has missing or extra files whose pieces overlap existing content, or a link-mode fallback leaves unsafe unmaterialized pieces. qui skips the match before adding to protect existing data. | See [Cross-seed skipped: "extra files share pieces with content"](#cross-seed-skipped-extra-files-share-pieces-with-content) and [Reflink Mode](./hardlink-mode.md#reflink-mode-alternative). |
| `below_threshold` | Skipped | The matched local files cover less than 95% of the release in hardlink or reflink mode. qui skips the match before it adds the torrent. | See [release matching](#release-didnt-match) and [Hardlink Mode](./hardlink-mode.md). This limit is fixed and is not a setting. |
| `requires_hardlink_reflink` | Skipped | The torrent layout scatters rootless or extra files in regular reuse mode. | Enable [Hardlink Mode](./hardlink-mode.md) or [Reflink Mode](./hardlink-mode.md#reflink-mode-alternative), or download the torrent normally. |
| `size_mismatch` | Failed | A search result already exists by infohash, but the earlier content prefilter rejected it because the torrent file list did not match the source sizes. | Compare the torrent files on the trackers. This protects you from treating different content as a valid cross-seed. See [release matching](#release-didnt-match). |
| `content_mismatch` | Failed | A search result already exists by infohash, but the earlier content prefilter rejected it for a non-size file-level reason. | Review the row message. See [How do I see why a release was filtered?](#how-do-i-see-why-a-release-was-filtered). |
| `hardlink_error` | Failed | You enabled hardlink mode, but qui failed to create or use the hardlink tree. | See [Hardlink mode failed](#hardlink-mode-failed) and [Hardlink Mode requirements](./hardlink-mode.md#requirements). |
| `reflink_error` | Failed | You enabled reflink mode, but qui failed to create or use the reflink tree. | See [Reflink mode failed](#reflink-mode-failed) and [Reflink Requirements](./hardlink-mode.md#reflink-requirements). |
| `no_save_path` | Failed | qui did not find a valid target save path for the cross-seed. The matched torrent has no usable SavePath and the category does not provide an explicit SavePath. | Check the matched torrent's save path and category save path in qBittorrent, then review [category behavior](./rules.md#category-behavior-details). |
| `error`, `alignment_failed`, or `pause_failed` | Failed | qBittorrent rejected the add, a required file or folder rename failed, or qui failed to pause a misaligned torrent after an alignment failure. | Check the instance connection, qBittorrent logs, and save path/category behavior in [Rules](./rules.md#category-behavior-details). |

Failed search or completion runs can trigger notification events. See [Notifications](../notifications.md#event-types) for the event keys.

:::tip
`size_mismatch` comes from the file sizes in torrent metadata. qui found a size difference between the source and candidate file lists. A different encode or layout can produce this status. The check does not compare piece hashes.

Compare both torrents' metadata and file lists before reporting bad data to a tracker. The `debug` log entry `[CROSSSEED-ASYNC] Starting async torrent analysis` includes the source torrent hash.
:::

:::tip
Use [piece boundary protection](./rules.md#matching) to protect content against bad hash torrents.
:::

## Why did my season-pack check return 404?

The season-pack check webhook returns `404 Not Found` whenever the pack is not ready to apply. In autobrr, this status appears as `[external webhook status code] not matching: got 404 want: 200`.

Common reasons:

- **Coverage is below your threshold**: qui did not find enough matching episodes
- **Episodes are still downloading**: only fully completed episode torrents count toward coverage
- **Release details do not match**: the episodes must match the pack's title, season, and normal release details such as source, resolution, and release group
- **No eligible instance was scanned**: the instance requires local filesystem access plus hardlink or reflink mode
- **Webhook source filters excluded your episodes**: include/exclude category or tag filters removed them from the scan
- **The release is not a season pack** or **season-pack matching is disabled**

If the pack matches except for REPACK, HDR, WEB, or year differences, check **Cross-Seed > Rules > Season packs > Matching settings**.

Open **Cross-Seed > Rules > Season packs** to review recent season-pack activity. The page displays the check/apply phase, status, reason, message, coverage, matched episodes, total episodes, selected instance, and link mode. You can also query `/api/cross-seed/season-pack/runs?limit=20` directly.

See [Season Packs](./season-packs.md) for the full flow, setup requirements, and season-pack-specific debugging steps.

## How do I see why a release was filtered?

For a manual search, open the **Search breakdown** section in the search dialog. If qui searched Torznab indexers, this breakdown appears under the results. A Gazelle-only search does not show it. It shows:

- One row for each indexer with its outcome: searched, incomplete, error, or excluded
- The count for each rejection reason
- The first rejected candidates for each reason, with the indexer, title, and size

Use **Copy report** to copy a plain-text summary for a bug report. The report contains the source torrent name and the rejected candidate titles.

The sections below cover the same decisions in the logs. If you run automatic tasks or need fully parsed release fields, use the logs.

qui logs rejection reasons at `DEBUG`, which is the default level:

```toml
logLevel = 'DEBUG'
```

If you upgraded from an older version, open `config.toml`. If `logLevel` holds another value, set it to `DEBUG`.

For **season-pack** checks, look for `[CROSSSEED-MATCH] Release filtered` entries. Each entry carries the `pack`, `season`, and `candidate` that qui compared. Each entry also has a `reason` field for the mismatch (for example `title mismatch`, `group mismatch`, `resolution mismatch`, `hdr mismatch`, `source mismatch`, `episode not in pack`, or `episode numbering mismatch`).

Two of these reasons show that the candidate belongs to different content. `title mismatch` indicates a different show. `episode not in pack` indicates an episode that this pack does not contain. Because these two reasons apply to most of a library, qui logs them at `TRACE`. All other reasons appear at `DEBUG`.

For regular cross-seed search, look for `[CROSSSEED-SEARCH] Candidate rejected` entries. Each entry names the indexer, the rejected candidate, the two sizes, and the reason. The entry `[CROSSSEED-SEARCH] Release filtering rejection summary` reports the count for each reason. `TRACE` adds `[CROSSSEED-SEARCH] Candidate rejected by search classifier`, which shows the parsed fields of both releases.

For content-prefilter decisions, `DEBUG` is enough. Look for messages such as:

- `crossseed: rejected existing content prefilter candidate after file-level matching`
- `[CROSSSEED-SEARCH] Late content filter exclusion`
- `[CROSSSEED-APPLY] Failed cached search selection already present after content prefilter rejection`
- `[CROSSSEED-SEARCH-AUTO] Existing search result failed due to prior content prefilter rejection`

For season-pack checks, `DEBUG` is often enough. Look for the torrent name and messages such as:

- `season pack: failed to resolve Sonarr season total`
- `season pack: metadata provider lookup failed`
- `load cached torrents for instance`
- `unsafe piece boundary with pending files`
- `torrent added paused; recheck queued`
- `Recheck completed below threshold, torrent left paused for manual review`

## When Rechecks Are Required (Reuse Mode)

In reuse mode (the default), qui adds most cross-seeds with hash verification skipped (`skip_checking=true`) and resumes them immediately. Some scenarios require a recheck:

### 1. Name or folder alignment needed

If the cross-seed torrent has a different display name or root folder, qui renames them to match. qBittorrent must run a recheck to verify the files at the new paths.

### 2. Extra files in source torrent

If the source torrent contains files that do not exist on disk (NFO, SRT, or samples that match no allowed extra-file pattern), a recheck determines the actual progress.

### 3. Hardlink/reflink filesystem fallback

If the source files and the link-tree base reside on different filesystems, or if the filesystem does not support the requested link type, link-tree creation fails. If you enable **Fallback to regular mode on error**, qui falls back to regular mode and adds the torrent against the matched source files instead of the link-tree directory.

qui treats these fallback torrents like disc-based content: it adds them paused, rechecks them, and auto-resumes only after qBittorrent reports 100% complete. If you enable **Skip recheck**, qui skips them instead. If you enable **Skip recheck**, disable **Fallback to regular mode**, because all fallbacks require a recheck.

If matches are partial-in-pack, size-based, renamed, or otherwise non-perfect, qui also runs piece-boundary protection before the fallback add. qui always enforces this check for link-mode fallback, even if the **Piece boundary safety check** switch in Rules is off. If the check fails, qui skips the torrent before adding it to qBittorrent.

### 4. Exact-size identity fallback

Sometimes two names describe the same bytes differently. One name uses a different codec, source, season, or episode number.

A fansub name also splits its release-group identity across two parsed fields. Exact positive reported sizes let qui consider these approved differences.

Title, season, episode, and split release-group differences require verification. qui adds these torrents paused and resumes them only after a 100% recheck.

Soft differences, such as codec, source, HDR, edition, or one-sided checksum data, keep the normal fast path after all safety checks.

If you enable **Skip recheck**, qui skips only decisions that require verification. RSS and autobrr reject these decisions before their planned download.

### Auto-resume behavior

- If missing data is at or below **Max auto-start download** (default: 50 MiB), qui auto-resumes after the recheck
- If only ignorable files are missing (samples, `.nfo`, subtitles), qui auto-resumes up to 200 MiB
- Torrents that miss more data stay paused for manual investigation
- Filesystem fallback, disc-layout, title-rescue, and exact-size identity matches require 100% completion before auto-resume
- Configure this limit with **Max auto-start download** in Rules

## Hardlink mode failed

Common causes:
- **Filesystem mismatch**: the hardlink base directory resides on a different filesystem or volume than the download paths. Hardlinks cannot cross filesystems.
- **Missing local filesystem access**: the target instance does not have "Local filesystem access" enabled in Instance Settings.
- **Permissions**: qui cannot read the instance content paths or write to the hardlink base directory.
- **Invalid base directory**: the hardlink base directory path does not exist and qui failed to create it.

## Directory permissions and umask

qui creates directories in your hardlink/reflink base (and cross-seed and dir-scan link trees) with mode `0777` and lets the process **umask** decide the final permissions. This follows the standard Unix pattern. It never makes anything world-writable by default. It only lets your umask control the group and other bits:

- umask `022` produces `0755` directories (`rwxr-xr-x`), the traditional default.
- umask `002` produces `0775` directories (`rwxrwxr-x`), which preserves group-write.

If qBittorrent and qui run as **different users that share a group** (a common hardlink/reflink setup), or if a tool like `fclones` deduplicates across the tree, those processes need group-write permissions on directories that qui creates. Set the umask to match, such as `UMASK=002`.

qui honors the `UMASK` environment variable (octal, for example `002`) and applies it at startup. It works on the official Docker image and on hotio or LinuxServer base images:

```bash
docker run -e UMASK=002 ... ghcr.io/autobrr/qui
```

:::note
If `UMASK` is unset, qui leaves the inherited umask unchanged. The official Docker image runs as root with default umask `022`. Without `UMASK`, it produces `0755` directories, the same as previous releases. `UMASK` is a no-op on Windows, where directory permissions follow inherited ACLs.
:::

The umask applies only to directories that qui creates moving forward. Existing directories keep their current permissions. If old permissions block access after you set `UMASK`, apply a one-time `chmod` to the existing tree, such as `chmod -R g+w /path/to/base`.

## Hardlink/reflink cross-seed shows "missing files"

If hardlink or reflink mode creates every file that the incoming torrent needs, qui usually adds the torrent with hash checking skipped and starts it immediately. Title rescue and exact-size season, episode, or release-group matches are exceptions. qui adds those torrents paused and requires a full recheck, because their names do not prove identical releases.

If qBittorrent still marks the torrent as `missing files`, the new torrent file most likely does not fully match the existing source and candidate files, even though qui matched them by name and size. Review the matching torrent group on the trackers before you resume or recheck the torrent, because one copy has corrupted hashes.
- **Hardlink mode**: If you resume the torrent, qBittorrent overwrites the data and corrupts other torrents that share those piece hashes.
- **Reflink mode**: If you resume the torrent, copy-on-write protects other torrents from data corruption.

:::tip
Report torrents with bad hashes at their relevant sites.
:::

:::warning
If you ignore the bad hashes in hardlink mode and resume, the torrents in the matching group repeat full rechecks and download bad pieces. This happens every time a peer requests the mismatched hashes and forces qBittorrent to validate.
:::

## "Files not found" after cross-seed (default mode)

In default mode, this error usually means the save path does not match the file location on disk:
- Ensure the cross-seed save path matches where the files exist
- Check the matched torrent save path in qBittorrent
- Ensure the matched torrent reached 100% download progress

## Reflink mode failed

Common causes:
- **Filesystem does not support reflinks**: the filesystem at the base directory does not support copy-on-write clones. On Linux, use BTRFS or XFS (with reflink enabled). On macOS, use APFS.
- **Pooled/virtual mount**: the base directory resides on a pooled or virtual filesystem (such as `mergerfs`, other FUSE mounts, or `overlayfs`) that does not implement reflink cloning. Use a direct disk mount for both your seeded data and the reflink base directory.
- **Filesystem mismatch**: the base directory resides on a different filesystem than the download paths.
- **Missing local filesystem access**: the target instance does not have "Local filesystem access" enabled.
- **Skip recheck enabled**: if reflink mode requires a recheck (extra files), qui skips the cross-seed.

## Cross-seed skipped: "extra files share pieces with content"

In regular reuse mode, this occurs if the **Piece boundary safety check** switch in Cross-Seed > Rules > Safety & validation is on (it is off by default). Link-mode fallback is stricter: for partial or otherwise non-perfect matches, qui always runs the check before adding the torrent to qBittorrent.

The incoming torrent contains files absent from your matched torrent, and those files share torrent pieces with your existing content. Downloading them can overwrite parts of your existing files.

**Solutions:**
- **Use reflink mode** (recommended): enable reflink mode for the instance. It clones the files, so qBittorrent modifies the clone without affecting the originals.
- **Disable the safety check**: turn off the **Piece boundary safety check** switch in Cross-Seed > Rules > Safety & validation (the default). If the content differs, the match proceeds, but it **can corrupt your existing seeded files**.
- If reflinks are unavailable and you want to avoid risk, download the torrent normally.

## Cross-seed stuck at low percentage after recheck

- Check if the source torrent contains extra files (NFO, samples) that do not exist on disk
- Check the "Max auto-start download" setting in Rules
- Torrents that miss more data than the limit stay paused for manual review

## Blu-ray or DVD cross-seed left paused

qui always adds torrents that contain disc-based media (Blu-ray `BDMV` or DVD `VIDEO_TS` folder structures) in a paused state.

**Why?** Disc layout torrents are sensitive to file alignment. Even minor path differences can cause qBittorrent to redownload large video segments and can corrupt your seeded content. Pausing these torrents lets you verify that the recheck reached 100% before you resume.

**What to do:**
1. If you enable **Skip recheck** in Cross-Seed Rules, qui skips disc-layout matches.
2. Otherwise, qui triggers a recheck automatically and auto-resumes only after the recheck reaches **100%**.
3. If auto-resume is disabled, resume the torrent manually after the recheck reaches 100%.

The result message states when this policy applies, for example: `"disc layout detected (BDMV), full recheck required"`

## Webhook returns HTTP 400 "invalid character" error

This error indicates that the torrent name contains special characters (such as double quotes `"`) that break JSON encoding. The error looks like:

```json
{"level":"error","error":"invalid character 'V' after object key:value pair","time":"...","message":"Failed to decode webhook check request"}
```

**Solution:** In your autobrr webhook configuration, use `toRawJson` instead of quoting the template variable directly:

```json
{
  "torrentName": {{ toRawJson .TorrentName }},
  "instanceIds": [1]
}
```

**Not:**
```json
{
  "torrentName": "{{ .TorrentName }}",
  "instanceIds": [1]
}
```

The `toRawJson` function (from Sprig) escapes special characters and outputs a valid JSON string, including quotes.

## Cross-seed in wrong category

- Check your cross-seed configuration in qui
- Ensure the matched torrent has the expected category
- For Dir Scan injections, Cross-Seed > Rules category modes do not apply. Dir Scan uses its own Default Category / Category override. If you leave it blank, the torrent receives no category.

## autoTMM unexpectedly enabled/disabled

- In reuse/affix mode (regular mode), autoTMM mirrors the setting of the matched torrent. This behavior is intentional.
- In affix mode, qui inherits autoTMM only when the cross-seed category and the matched torrent share a save path. Otherwise qui turns autoTMM off and sets an explicit `savepath`.
- In indexer name or custom category mode, qui always disables autoTMM.
- In hardlink/reflink mode, qui always disables autoTMM (explicit `savepath`).
- Dir Scan injections always disable autoTMM (explicit `savepath`).
- Check the autoTMM status of the original torrent in qBittorrent.
