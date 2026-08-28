---
sidebar_position: 2
title: Rules
description: Rules that decide which cross-seed candidates qui adds.
---

# Cross-Seed Rules

Configure matching behavior in the **Rules** tab on the Cross-Seed page.

## Matching

- **Cross-seed episodes from packs**: If enabled, season packs also match individual episodes. If disabled, season packs only match other season packs. qui adds episodes with AutoTMM disabled to prevent save path conflicts.
- **Skip recheck**: If enabled, qui skips any cross-seed that requires a recheck. This includes renamed paths, extra files, filesystem fallback, disc layouts, title rescue, and exact-size matches with different season, episode, or release-group details. This rule applies to regular, hardlink, and reflink modes.
- **Rescue title mismatches**: Disabled by default. If only the title differs and the positive reported size matches, this rule tests that result. Each source search tries at most three rescue downloads across all indexers. RSS and autobrr use this rule only when the announcement provides an exact size. qui adds rescued torrents in a paused state and starts them only after a full qBittorrent recheck reaches 100%. **Skip recheck** disables this rule.
- **Piece boundary safety check**: Off by default. Turn the switch on to block cross-seeds whose extra files share torrent pieces with content files. With the switch off, qBittorrent can corrupt your existing seeded data if the content differs. Reflink mode protects the original files. If hardlink or reflink mode falls back to regular mode, qui runs the check even when the switch is off. This fallback check covers matches that are not exact, need renames, or have extra files.

:::note
If a torrent uses filesystem fallback, disc layouts (`BDMV`/`VIDEO_TS`), title rescue, or exact-size season, episode, or release-group matches, qui auto-resumes it only after a full recheck reaches 100%.
:::

### Reported-size fallback

Strict release matching runs first. If qui finds an exact positive reported byte count, it relaxes approved name differences.

Reported size does not prove that two torrents contain the same bytes. qui still checks the torrent metadata, files, paths, layout, and piece boundaries.

If torrents have title, season, episode, or split release-group differences, qui requires a full piece check. qui adds these torrents paused and resumes them only at 100%.

Soft descriptive differences keep the normal fast path. These differences include codec, source, HDR, edition, and one-sided checksum data.

**Skip recheck** removes only matches that need verification. RSS and autobrr reject those matches before the planned torrent download.

## Search Category Rules

qui reads the torrent name to find the content type. It then corrects that guess with the file extensions inside the torrent. Both signals can fail.

A search category rule forces the content type for every torrent in a qBittorrent category. The rule overrides the name and the file extensions.

The content type decides which Torznab categories qui requests, and which search mode it uses. As a result, it decides which indexers can answer the search.

### When a rule helps

File extensions cannot always correct the name:

- A disc image contains one `.iso` file. Because this extension is neither audio nor video, the extensions give no signal and the name decides alone.
- An ebook contains one `.epub` or `.pdf` file. A name such as `Author Name - Book Title (2021)` can parse as music or as a movie.

In both examples, the torrent is in a category that you control. A rule on that category gives qui the correct content type.

### Add a rule

1. Open the **Rules** tab on the Cross-Seed page.
2. Find **Search category rules** under the **Matching** heading.
3. Select **Add rule**.
4. Select or type one or more qBittorrent categories.
5. Select the content type in the **search as** list.

The content types are Movie, TV, Music, Audiobook, Book, Comic, Game, and App.

### How rules match

One rule can hold more than one category. A torrent in any of those categories gets the content type of that rule.

The match is exact and case-sensitive because qBittorrent categories are case-sensitive. A rule for `ebooks` does not match a torrent in `Ebooks`.

If two rules name the same category, the first rule keeps it. When qui saves the settings, it removes that category from the later rule. If this empties the later rule, qui removes that rule.

:::note
Manual search, Library Scan, completion search, RSS matching, and autobrr matching apply these rules to local source torrents. Dir Scan uses its own detection.
:::

:::note
Audiobook and Music request the same categories from indexers, and both send an artist and an album parameter. Only the text of the search query differs.
:::

## Season Pack Threshold

The season-pack webhook uses a separate coverage threshold (default 75%) to decide whether enough local data exists to inject a pack. qui gets season episode totals from Sonarr first. If Sonarr cannot resolve the release, qui uses TVDB or TVMaze. If torrent data is available, qui never uses a total lower than the playable file count in the pack torrent. qui adds incomplete packs paused and rechecks them. When the recheck reports progress close to the share of bytes qui linked, qui resumes the pack. If progress lands well below that share, the links failed and the pack stays paused for manual review. Configure this in **Rules > Season packs**. Instances must have local filesystem access and hardlink or reflink mode enabled to qualify. See [Season Packs](./season-packs.md) for details.

Season-pack matching rules live in **Rules > Season packs** and affect every season pack flow: the autobrr webhook, automatic assembly, and library search runs.

## Categories

These modes set the category that qui gives to a new cross-seed. To choose the search content type from the category of the source torrent, see [Search Category Rules](#search-category-rules).

Choose one of four mutually exclusive category modes:

### Reuse matched torrent category

Keeps the category of the matched torrent unchanged. qui adds no affix.

### Category Affix (default)

Adds a configurable affix to the category of the matched torrent. This prevents Sonarr and Radarr from importing cross-seeded files as duplicates. If you use **regular mode** (no hardlink/reflink), the cross-seed inherits AutoTMM from the matched torrent.

**Affix Mode:**

- **Suffix** (default): Appends the affix to the category (for example `movies` → `movies.cross`)
- **Prefix**: Prepends the affix to the category (for example `movies` → `cross/movies`)

**Affix Value:** The text to add (default: `.cross`). Common examples:

- `.cross` with suffix mode → `tv.cross`, `movies.cross`
- `cross/` with prefix mode → `cross/tv`, `cross/movies`

:::tip
If you use prefix mode with a trailing `/`, qBittorrent creates nested categories<sup>1</sup>. This groups all cross-seeds under a parent category. A filter on `cross` returns all cross-seeds (`cross/movies`, `cross/tv`, and more).
:::

:::warning
Do not use a leading `/` in suffix mode (for example `/cross-seed`). This creates the cross-seed as a **child** of the original category<sup>1</sup>. A `movies` category in Radarr then also returns `movies/cross-seed` torrents. This causes conflicts.

If you want nested categories, use prefix mode instead.
:::

*<sup>1</sup> Nested categories require you to enable subcategories (Instance Preferences → Files → Enable Subcategories).*

### Use indexer name as category

Sets the category to the indexer name (for example `TorrentDB`). qui always disables AutoTMM and uses explicit save paths.

### Custom category

Uses a fixed category name for all cross-seeds (for example `cross-seed`). qui always disables AutoTMM and uses explicit save paths.

## Source Tagging

Configure the tags that qui applies to cross-seed torrents, based on the discovery method:

| Tag Setting | Description | Default |
|-------------|-------------|---------|
| RSS Automation Tags | Torrents added via RSS feed polling | `["cross-seed"]` |
| Seeded Search Tags | Torrents added via seeded torrent search | `["cross-seed"]` |
| Completion Search Tags | Torrents added via completion-triggered search | `["cross-seed"]` |
| Webhook Tags | Torrents added via `/apply` webhook | `["cross-seed"]` |
| Inherit source torrent tags | Also copy tags from the matched source torrent | - |

## Max Auto-Start Download

After a recheck, qui reads how much data the new cross-seed still lacks. If the missing data is at or below **Max auto-start download** (default: 50 MiB), qui starts the torrent. Torrents that lack more data stay paused for manual review. Set 0 to start only fully complete torrents.

If only ignorable files are missing (samples, `.nfo`, subtitles, and similar sidecar files), qui starts the torrent anyway. This exception has a fixed 200 MiB ceiling.

This limit applies to new cross-seed additions from RSS, seeded search, completion search, and the webhooks. The season-pack flow and Dir Scan use their own resume rules and ignore this limit.

## External Program

qui can run an external program after it injects a cross-seed torrent.

## Category Behavior Details

### autoTMM (Auto Torrent Management)

The active category mode determines autoTMM behavior:

| Category Mode | autoTMM Behavior |
|---------------|------------------|
| **Reuse matched category** | Inherited from matched torrent (regular mode only, hardlink/reflink disables autoTMM) |
| **Category Affix** | Inherited from matched torrent when the affix category has the same save path (regular mode only, hardlink/reflink disables autoTMM) |
| **Indexer name** | Always disabled (explicit save paths) |
| **Custom** | Always disabled (explicit save paths) |

If the cross-seed inherits autoTMM in reuse or affix mode:

- If the matched torrent uses autoTMM, the cross-seed uses autoTMM
- If the matched torrent has a manual path, the cross-seed uses the same manual path

If autoTMM is disabled (indexer and custom modes), qui gives cross-seeds explicit save paths derived from the location of the matched torrent.

:::note
Hardlink/reflink mode always adds torrents with an explicit `savepath` that points at the link tree. This forces autoTMM off.
Dir Scan injections are separate from cross-seed rules and also always add with explicit `savepath` (autoTMM off).
:::

### Save path determination

Priority order:

1. Base category's explicit save path (if configured in qBittorrent)
2. Matched torrent's current save path (fallback)

**Examples:**

*Suffix mode (default):*

- The `tv` category has save path `/data/tv`
- The cross-seed gets the `tv.cross` category with save path `/data/tv`
- qui finds the files because they are in the same location

*Prefix mode:*

- The `movies` category has save path `/data/movies`
- The cross-seed gets the `cross/movies` category with save path `/data/movies`
- The nested `cross/` parent in qBittorrent groups all cross-seeds together

## Best practices

**Do:**

- Use autoTMM consistently across your torrents
- Let qui create cross-seed categories automatically
- Keep category structures simple
- If you want all cross-seeds grouped under one parent category, use prefix mode with `/` (for example `cross/`)

**Do not:**

- Manually move torrent files after you add them
- Create cross-seed categories manually with different paths
- Mix autoTMM and manual paths for the same content type
