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

### Season pack vs episodes

By default, season packs only match other season packs. Enable **Find individual episodes** in settings to allow season packs to match individual episode releases.

## How do I see why a release was filtered?

Enable trace logging to see detailed rejection reasons:

```toml
loglevel = 'TRACE'
```

Look for `[CROSSSEED-MATCH] Release filtered` entries showing exactly which field caused the mismatch (e.g., `group_mismatch`, `resolution_mismatch`, `language_mismatch`).

## When Rechecks Are Required (Reuse Mode)

In reuse mode (the default), most cross-seeds are added with hash verification skipped (`skip_checking=true`) and resume immediately. Some scenarios require a recheck:

### 1. Name or folder alignment needed

When the cross-seed torrent has a different display name or root folder, qui renames them to match. qBittorrent must recheck to verify files at the new paths.

### 2. Extra files in source torrent

When the source torrent contains files not on disk (NFO, SRT, samples not matching allowed extra file patterns), a recheck determines actual progress.

### Auto-resume behavior

- Default tolerance 5% → auto-resumes at ≥95% completion
- Torrents below threshold stay paused for manual investigation
- Configure via **Size mismatch tolerance** in Rules

## Hardlink mode failed

Common causes:
- **Filesystem mismatch**: Hardlink base directory is on a different filesystem/volume than the download paths. Hardlinks cannot cross filesystems.
- **Missing local filesystem access**: The target instance doesn't have "Local filesystem access" enabled in Instance Settings.
- **Permissions**: qui cannot read the instance's content paths or write to the hardlink base directory.
- **Invalid base directory**: The hardlink base directory path doesn't exist and couldn't be created.

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

This only occurs when you have enabled the piece boundary safety check (disabled "Skip piece boundary safety check" in Rules).

The incoming torrent has files not present in your matched torrent, and those files share torrent pieces with your existing content. Downloading them could overwrite parts of your existing files.

**Solutions:**
- **Use reflink mode** (recommended): Enable reflink mode for the instance—it safely clones files so qBittorrent can modify them without affecting originals
- **Disable the safety check**: Check "Skip piece boundary safety check" in Rules (the default). The match will proceed but **may corrupt your existing seeded files** if content differs
- If reflinks aren't available and you want to avoid any risk, download the torrent fresh

## Cross-seed stuck at low percentage after recheck

- Check if the source torrent has extra files (NFO, samples) not present on disk
- Verify the "Size mismatch tolerance" setting in Rules
- Torrents below the auto-resume threshold stay paused for manual review

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

## autoTMM unexpectedly enabled/disabled

- In affix mode, autoTMM mirrors the matched torrent's setting (intentional)
- In indexer name or custom category mode, autoTMM is always disabled
- Check the original torrent's autoTMM status in qBittorrent
