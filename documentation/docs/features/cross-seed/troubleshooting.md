---
sidebar_position: 7
title: Troubleshooting
---

# Cross-Seed Troubleshooting

## Why didn't my cross-seed get added?

### Rate limiting (HTTP 429)

Indexers limit how frequently you can make requests. If you see errors like `"indexer TorrentLeech rate-limited until..."`, qui has recorded the cooldown and will skip that indexer until it's available. Check the **Scheduler Activity** panel on the Indexers page to see which indexers are in cooldown and when they'll be ready.

### Release didn't match

qui uses strict matching to ensure cross-seeds have identical files. Both releases must match on:
- Title, year, and release group
- Resolution (1080p, 2160p)
- Source (WEB-DL, BluRay) and collection (AMZN, NF)
- Codec (x264, x265) and HDR format
- Audio format and channels
- Language, edition, cut, and version (v2, v3)
- Variants like IMAX, HYBRID, REPACK, PROPER

Some discovery methods can use an exact reported byte count when strict release matching rejects an approved metadata difference.

Interactive Torznab search, Library Scan, and completion search share this rule. RSS uses the title and byte count from the feed before its one download.

The autobrr `/check` endpoint uses `.Size` without fetching the torrent file. The value can be exact, rounded, or `0`.

If autobrr sends `0`, qui uses a narrow name-only preflight. The `/apply` endpoint calculates the actual size and repeats the match against live sources.

An exact reported size is evidence, not byte verification. qui still checks the downloaded torrent metadata, files, layout, and piece boundaries.

### Season pack vs episodes

By default, season packs only match other season packs. Enable **Find individual episodes** in settings to allow season packs to match individual episode releases.

### Prowlarr filters remove expected results

qui searches the selected Prowlarr indexer directly. If that indexer has a search filter enabled, such as freeleech only, the filter also applies to cross-seed searches. The tracker can return no results even when matching cross-seed candidates exist.

Prowlarr does not support per-search filter overrides. Add a second entry for the tracker in Prowlarr with the filter disabled, then select that second entry in qui.

## Cross-seed search run statuses

Library scan and completion search rows use **added**, **skipped**, or **failed** as the top-level outcome. Open the row details to see the per-attempt status and message.

| Status or message | Outcome | What it usually means | What to check |
| --- | --- | --- | --- |
| `exists` | Skipped | The exact torrent infohash is already in the target qBittorrent instance. | This is normally harmless. If you expected a new tracker result, check the source and target indexers in [Cross-Seed Overview](./overview.md#discovery-methods). |
| `no_match` | Skipped | qui searched but did not find an existing local torrent with the required files. | Review [release matching](#release-didnt-match), source filters, and the discovery method in [Library Scan](./overview.md#library-scan) or [Auto-Search on Completion](./overview.md#auto-search-on-completion). |
| `blocked` | Skipped | The candidate infohash is on the cross-seed blocklist. | Remove it from **Cross-Seed > Blocklist** if you want qui to try it again. See [Blocklist](./overview.md#blocklist). |
| `skipped_recheck` | Skipped | The match would require a recheck, but **Skip recheck** is enabled. | See [When Rechecks Are Required](#when-rechecks-are-required-reuse-mode) and [Rules](./rules.md#matching). |
| `skipped_unsafe_pieces` | Skipped | The incoming torrent has missing or extra files whose pieces overlap existing content, or a link-mode fallback would leave unsafe unmaterialized pieces. qui skips before adding to avoid modifying existing data. | See [Cross-seed skipped: "extra files share pieces with content"](#cross-seed-skipped-extra-files-share-pieces-with-content) and [Reflink Mode](./hardlink-mode.md#reflink-mode-alternative). |
| `below_threshold` | Skipped | The matched local files cover less than 95% of the release in hardlink or reflink mode. qui skips the match before it adds the torrent. | See [release matching](#release-didnt-match) and [Hardlink Mode](./hardlink-mode.md). This limit is fixed and is not a setting. |
| `requires_hardlink_reflink` | Skipped | The torrent layout would scatter rootless or extra files in regular reuse mode. | Enable [Hardlink Mode](./hardlink-mode.md) or [Reflink Mode](./hardlink-mode.md#reflink-mode-alternative), or download the torrent normally. |
| `size_mismatch` | Failed | A search result already exists by infohash, but the earlier content prefilter rejected it because the torrent file list did not match the source sizes. | Compare the torrent files on the trackers. This protects you from treating different content as a valid cross-seed. See [release matching](#release-didnt-match). |
| `content_mismatch` | Failed | A search result already exists by infohash, but the earlier content prefilter rejected it for a non-size file-level reason. | Review the row message. See [How do I see why a release was filtered?](#how-do-i-see-why-a-release-was-filtered). |
| `hardlink_error` | Failed | Hardlink mode was enabled but qui could not create or use the hardlink tree. | See [Hardlink mode failed](#hardlink-mode-failed) and [Hardlink Mode requirements](./hardlink-mode.md#requirements). |
| `reflink_error` | Failed | Reflink mode was enabled but qui could not create or use the reflink tree. | See [Reflink mode failed](#reflink-mode-failed) and [Reflink Requirements](./hardlink-mode.md#reflink-requirements). |
| `no_save_path` | Failed | qui could not find a valid target save path for the cross-seed. The matched torrent has no usable SavePath and the category does not provide an explicit SavePath. | Verify the matched torrent's save path and category save path in qBittorrent, then review [category behavior](./rules.md#category-behavior-details). |
| `error`, `alignment_failed`, or `pause_failed` | Failed | qBittorrent rejected the add, a required file or folder rename failed, or qui could not pause a misaligned torrent after an alignment failure. | Check the instance connection, qBittorrent logs, and save path/category behavior in [Rules](./rules.md#category-behavior-details). |

Failed search or completion runs can trigger notification events. See [Notifications](../notifications.md#event-types) for the event keys.

:::tip
`size_mismatch` failures are generated from the size reported inside of torrent files, not the content on disk. These failures are strong indicators that the cross seeded content has mismatching piece hashes between trackers. One or more trackers had a bad hash copy.

The failures are the size mismatches against the selected source torrent used for cross seed searching (typically content in a folder), they are not reports of which trackers actually have bad hashes.
If the source torrent is the bad hash, the hash in `debug` logging `[CROSSSEED-ASYNC] Starting async torrent analysis` shows the source hash that was used.
:::

:::tip
Use [piece boundary protection](./rules.md#matching) to protect content against bad hash torrents.
:::

## Why did my season-pack check return 404?

The season-pack check webhook returns `404 Not Found` whenever the pack is not ready to apply. In autobrr this usually appears as `[external webhook status code] not matching: got 404 want: 200`.

Common reasons:

- **Coverage is below your threshold**: qui did not find enough matching episodes
- **Episodes are still downloading**: only fully completed episode torrents count toward coverage
- **Release details do not match**: the episodes must match the pack's title, season, and normal release details such as source, resolution, and release group
- **No eligible instance was scanned**: the instance needs local filesystem access plus hardlink or reflink mode
- **Webhook source filters excluded your episodes**: include/exclude category or tag filters removed them from the scan
- **The release is not a season pack** or **season-pack matching is disabled**

If the pack should match except for REPACK, HDR, WEB, or year differences, check **Cross-Seed > Rules > Season packs > Matching settings**.

Open **Cross-Seed > Rules > Season packs** for recent season-pack activity. It shows the check/apply phase, status, reason, message, coverage, matched episodes, total episodes, selected instance, and link mode. You can also query `/api/cross-seed/season-pack/runs?limit=20` directly.

See [Season Packs](./season-packs.md) for the full flow, setup requirements, and season-pack-specific debugging steps.

## How do I see why a release was filtered?

For a manual search, open the **Search breakdown** section in the search dialog. It appears under the results when Qui searched Torznab indexers. A Gazelle-only search does not show it. It shows:

- One row for each indexer with its outcome: searched, incomplete, error, or excluded
- The count for each rejection reason
- The first rejected candidates for each reason, with the indexer, title, and size

Use **Copy report** to copy a plain-text summary for a bug report. The report contains the source torrent name and the rejected candidate titles.

The sections below cover the same decisions in the logs. Use the logs for automatic runs, or when you need the fully parsed release fields.

Rejection reasons are logged at `DEBUG`, which is the default level:

```toml
logLevel = 'DEBUG'
```

If you upgraded from an older version, open `config.toml`. If `logLevel` holds another value, set it to `DEBUG`.

For **season-pack** checks, look for `[CROSSSEED-MATCH] Release filtered` entries. Each entry carries the `pack`, `season`, and `candidate` that qui compared. Each entry also has a `reason` field for the mismatch (for example `title mismatch`, `group mismatch`, `resolution mismatch`, `hdr mismatch`, `source mismatch`, `episode not in pack`, or `episode numbering mismatch`).

Two of these reasons show that the candidate belongs to different content. `title mismatch` means a different show. `episode not in pack` means an episode that this pack does not contain. These two reasons apply to most of a library, thus qui logs them at `TRACE`. All other reasons appear at `DEBUG`.

For regular cross-seed search, look for `[CROSSSEED-SEARCH] Candidate rejected`. Each entry names the indexer, the rejected candidate, the two sizes, and the reason. The entry `[CROSSSEED-SEARCH] Release filtering rejection summary` reports the count for each reason. `TRACE` adds `[CROSSSEED-SEARCH] Candidate rejected by search classifier`, which shows the parsed fields of both releases.

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

In reuse mode (the default), most cross-seeds are added with hash verification skipped (`skip_checking=true`) and resume immediately. Some scenarios require a recheck:

### 1. Name or folder alignment needed

When the cross-seed torrent has a different display name or root folder, qui renames them to match. qBittorrent must recheck to verify files at the new paths.

### 2. Extra files in source torrent

When the source torrent contains files not on disk (NFO, SRT, samples not matching allowed extra file patterns), a recheck determines actual progress.

### 3. Hardlink/reflink filesystem fallback

When link-tree creation fails because the source files and link-tree base are on different filesystems, or because the filesystem does not support the requested link type, qui can fall back to regular mode if **Fallback to regular mode** is enabled. The torrent is added against the matched source files, not the link-tree directory.

These fallback torrents are treated like disc-based content: they are added paused, rechecked, and only auto-resume after qBittorrent reports 100% complete. If **Skip recheck** is enabled, qui skips them instead. With **Skip recheck** enabled, a better workflow would have **Fallback to regular mode** disabled, since all fallbacks require recheck.

For partial-in-pack, size-based, renamed, or otherwise non-perfect matches, qui also runs piece-boundary protection before the fallback add. This check is always enforced for link-mode fallback, even when **Skip piece boundary safety check** is enabled for regular reuse mode. If the check fails, qui skips the torrent before adding it to qBittorrent.

### 4. Exact-size identity fallback

Sometimes two names describe the same bytes differently. One name can use a different codec, source, season, or episode number.

A fansub name can also split its release-group identity across two parsed fields. Exact positive reported sizes let qui consider these approved differences.

Title, season, episode, and split release-group differences require verification. qui adds these torrents paused and resumes them only after a 100% recheck.

Soft differences, such as codec, source, HDR, edition, or one-sided checksum data, can keep the normal fast path after all safety checks.

If **Skip recheck** is enabled, qui skips only decisions that require verification. RSS and autobrr reject these decisions before their planned download.

### Auto-resume behavior

- After the recheck, qui auto-resumes only when the missing data is at or below **Max auto-start download** (default: 50 MiB)
- When only ignorable files are missing (samples, `.nfo`, subtitles), qui auto-resumes anyway, up to 200 MiB
- Torrents that miss more data stay paused for manual investigation
- Filesystem fallback, disc-layout, title-rescue, and exact-size identity matches require 100% completion before auto-resume
- Configure via **Max auto-start download** in Rules

## Hardlink mode failed

Common causes:
- **Filesystem mismatch**: Hardlink base directory is on a different filesystem/volume than the download paths. Hardlinks cannot cross filesystems.
- **Missing local filesystem access**: The target instance doesn't have "Local filesystem access" enabled in Instance Settings.
- **Permissions**: qui cannot read the instance's content paths or write to the hardlink base directory.
- **Invalid base directory**: The hardlink base directory path doesn't exist and couldn't be created.

## Directory permissions and umask

qui creates the directories in your hardlink/reflink base (and cross-seed/dir-scan link trees) with mode `0777` and then lets the process **umask** decide the final permissions. This is the standard Unix pattern. It never makes anything world-writable by default, it only lets your umask control the group and other bits:

- umask `022` produces `0755` directories (`rwxr-xr-x`), the traditional default.
- umask `002` produces `0775` directories (`rwxrwxr-x`), preserving group-write.

If qBittorrent and qui run as **different users that share a group** (a common hardlink/reflink setup), or a tool like `fclones` deduplicates across the tree, those processes need group-write on the directories qui creates. Set the umask accordingly, for example `UMASK=002`.

qui honors the `UMASK` environment variable (octal, e.g. `002`) and applies it at startup, so it works on the official Docker image as well as hotio or LinuxServer base images:

```bash
docker run -e UMASK=002 ... ghcr.io/autobrr/qui
```

:::note
When `UMASK` is unset, qui leaves the inherited umask unchanged. The official Docker image runs as root with the default umask `022`, so without `UMASK` it produces `0755` directories, matching previous releases. `UMASK` is a no-op on Windows, where directory permissions follow inherited ACLs.
:::

The umask only applies to directories qui creates from now on. Directories that already exist keep their current permissions, so after setting `UMASK` you may need a one-time `chmod` over the existing tree, for example `chmod -R g+w /path/to/base`.

## Hardlink/reflink cross-seed shows "missing files"

When hardlink or reflink mode creates every file needed by the incoming torrent, qui usually adds it with hash checking skipped and starts it immediately. Title rescue and exact-size season, episode, or release-group matches are exceptions. qui adds those torrents paused and requires a full recheck because their names did not prove that the releases are identical.

If qBittorrent still marks the torrent as `missing files`, the new torrent file most likely does not fully match the existing source/candidate files, even though qui matched them by name and size. Review the matching torrent group on the tracker/s before resuming or rechecking the torrent, as one of the copies has corrupted hash/es.
- **Hardlink mode**: Resuming the torrent will overwrite the bad hashes for that torrent, corrupting the existing torrent/s with the other piece hash/es.
- **Reflink mode**: Resuming the torrent will leverage copy-on-write to protect the other torrent hash/es.

:::tip
Torrents with bad hash/es should be reported at their relevant sites.
:::

:::warning
Ignoring the bad hash/es in hardlink mode and resuming, will cause repeated full torrent rechecks, and downloading bad pieces, on torrents in the matching group, every time a peer requests the mis-matched hash/es and forces qBitTorrent to validate.
:::

## "Files not found" after cross-seed (default mode)

This typically occurs in default mode when the save path doesn't match where files actually exist:
- Check that the cross-seed's save path matches where files actually exist
- Verify the matched torrent's save path in qBittorrent
- Ensure the matched torrent has completed downloading (100% progress)

## Reflink mode failed

Common causes:
- **Filesystem doesn't support reflinks**: The filesystem at the base directory doesn't support copy-on-write clones. On Linux, use BTRFS or XFS (with reflink enabled). On macOS, use APFS.
- **Pooled/virtual mount**: The base directory is on a pooled/virtual filesystem (like `mergerfs`, other FUSE mounts, or `overlayfs`) which often does not implement reflink cloning. Use a direct disk mount for both your seeded data and the reflink base directory.
- **Filesystem mismatch**: Base directory is on a different filesystem than the download paths.
- **Missing local filesystem access**: The target instance doesn't have "Local filesystem access" enabled.
- **SkipRecheck enabled**: If reflink mode would require recheck (extra files), it skips the cross-seed.

## Cross-seed skipped: "extra files share pieces with content"

In regular reuse mode, this occurs when you have enabled the piece boundary safety check (disabled "Skip piece boundary safety check" in Rules). Link-mode fallback is stricter: for partial or otherwise non-perfect matches, qui always performs the check before adding the torrent to qBittorrent.

The incoming torrent has files not present in your matched torrent, and those files share torrent pieces with your existing content. Downloading them could overwrite parts of your existing files.

**Solutions:**
- **Use reflink mode** (recommended): Enable reflink mode for the instance—it safely clones files so qBittorrent can modify them without affecting originals
- **Disable the safety check**: Check "Skip piece boundary safety check" in Rules (the default). The match will proceed but **may corrupt your existing seeded files** if content differs
- If reflinks aren't available and you want to avoid any risk, download the torrent fresh

## Cross-seed stuck at low percentage after recheck

- Check if the source torrent has extra files (NFO, samples) not present on disk
- Check the "Max auto-start download" setting in Rules
- Torrents that miss more data than the limit stay paused for manual review

## Blu-ray or DVD cross-seed left paused

Torrents containing disc-based media (Blu-ray `BDMV` or DVD `VIDEO_TS` folder structures) are always added paused.

**Why?** Disc layout torrents are sensitive to file alignment. Even minor path differences can cause qBittorrent to redownload large video segments, potentially corrupting your seeded content. Leaving them paused lets you verify the recheck completed at 100% before resuming.

**What to do:**
1. If **Skip recheck** is enabled in Cross-Seed Rules, disc-layout matches will be skipped.
2. Otherwise, qui triggers a recheck automatically and will only auto-resume once the recheck reaches **100%**.
3. If you have auto-resume disabled, resume manually after verifying it reaches 100%.

The result message will indicate when this policy applies (example): `"disc layout detected (BDMV), full recheck required"`

## Webhook returns HTTP 400 "invalid character" error

This typically means the torrent name contains special characters (like double quotes `"`) that break JSON encoding. The error often looks like:

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

The `toRawJson` function (from Sprig) properly escapes special characters and outputs a valid JSON string including the quotes.

## Cross-seed in wrong category

- Check your cross-seed settings in qui
- Verify the matched torrent has the expected category
- For Dir Scan injections, Cross-Seed → Rules category modes do not apply. Dir Scan uses its own Default Category / Category override, and leaving it blank results in no category.

## autoTMM unexpectedly enabled/disabled

- In reuse/affix mode (regular mode), autoTMM mirrors the matched torrent's setting (intentional)
- In indexer name or custom category mode, autoTMM is always disabled
- In hardlink/reflink mode, autoTMM is always disabled (explicit `savepath`)
- Dir Scan injections always disable autoTMM (explicit `savepath`)
- Check the original torrent's autoTMM status in qBittorrent
