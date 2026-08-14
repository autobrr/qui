---
sidebar_position: 2
title: Rules
---

# Cross-Seed Rules

Configure matching behavior in the **Rules** tab on the Cross-Seed page.

## Matching

- **Find individual episodes** - When enabled, season packs also match individual episodes. When disabled, season packs only match other season packs. Episodes are added with AutoTMM disabled to prevent save path conflicts.
- **Skip recheck** - When enabled, skips any cross-seed that would require a recheck (alignment needed, extra files, filesystem fallback, or disc layouts like `BDMV`/`VIDEO_TS`). Applies to all modes including hardlink/reflink.
- **Rescue title mismatches** - Disabled by default. This rule can try exact-size results when only the title differs. Each source search can try at most three rescue downloads across all indexers. After download, every usable file must have one exact-size partner. qui adds a rescued torrent paused. It starts only after a full qBittorrent recheck reaches 100%. A rescue that fails still counts as a download on your tracker. **Skip recheck** turns off this rule. Manual search, Library Scan, and completion search use it. RSS, webhooks, and direct apply requests do not use it.
- **Skip piece boundary safety check** - Enabled by default. When enabled, allows cross-seeds even if extra files share torrent pieces with content files. **Warning:** This may corrupt your existing seeded data if content differs. Uncheck this to enable the safety check, or use reflink mode which safely handles these cases.

:::note
Filesystem fallback and disc layouts (`BDMV`/`VIDEO_TS`) are treated more strictly: they only auto-resume after a full recheck reaches 100%.
:::

## Search Category Rules

qui reads the torrent name to find the content type. It then corrects that guess with the file extensions inside the torrent. Both signals can be wrong.

A search category rule forces the content type for every torrent in a qBittorrent category. The rule wins over the name and over the file extensions.

The content type decides which Torznab categories qui asks for, and which search mode it uses. It therefore decides which indexers can answer the search.

### When a rule helps

File extensions cannot always correct the name:

- A disc image holds one `.iso` file. This extension is neither audio nor video, so the extensions give no signal and the name decides alone.
- An ebook is one `.epub` or `.pdf` file. A name such as `Author Name - Book Title (2021)` can parse as music or as a movie.

In both examples the torrent is in a category that you control. A rule on that category gives qui the correct content type.

### Add a rule

1. Open the **Rules** tab on the Cross-Seed page.
2. Find **Search category rules** in the **Matching behavior** card.
3. Select **Add rule**.
4. Select or type one or more qBittorrent categories.
5. Select the content type in the **search as** list.

The content types are Movie, TV, Music, Audiobook, Book, Comic, Game, and App.

### How rules match

One rule can hold more than one category. A torrent in any of those categories gets the content type of that rule.

The match is exact and case-sensitive, because qBittorrent categories are case-sensitive. A rule for `ebooks` does not match a torrent in `Ebooks`.

If two rules name the same category, the first rule keeps it. When qui saves the settings, it removes that category from the later rule. If this empties the later rule, qui removes that rule.

:::note
Manual search, Library Scan, and completion search use these rules. RSS, webhooks, and direct apply requests do not use them. Those flows have no qBittorrent source torrent, so they have no category to match. Dir Scan has its own detection and does not use these rules.
:::

:::note
Audiobook and Music ask the indexers for the same categories, and both send an artist and an album parameter. Only the text of the search query differs.
:::

## Season Pack Threshold

The season-pack webhook uses a separate coverage threshold (default 75%) to decide whether enough local data exists to inject a pack. Season episode totals are sourced from Sonarr first, then TVDB or TVMaze when Sonarr cannot resolve the release. When torrent data is available, qui never uses a total lower than the playable file count in the pack torrent. Incomplete packs are added paused, rechecked, then resumed automatically when qBittorrent reports progress at or above the season-pack threshold. This is configured in **Rules > Season packs**. Instances must have local filesystem access and hardlink or reflink mode enabled to qualify. See [Season Packs](./season-packs.md) for details.

Season-pack matching rules live in **Rules > Season packs** and affect only the season-pack webhook flow.

## Categories

These modes set the category that qui gives to a new cross-seed. To choose the search content type from the category of the source torrent, see [Search Category Rules](#search-category-rules).

Choose one of three mutually exclusive category modes:

### Category Affix (default)

Adds a configurable affix to the matched torrent's category. Prevents Sonarr/Radarr from importing cross-seeded files as duplicates. In **regular mode** (no hardlink/reflink), AutoTMM is inherited from the matched torrent.

**Affix Mode:**
- **Suffix** (default): Appends the affix to the category (e.g., `movies` → `movies.cross`)
- **Prefix**: Prepends the affix to the category (e.g., `movies` → `cross/movies`)

**Affix Value:** The text to add (default: `.cross`). Common examples:
- `.cross` using suffix mode → `tv.cross`, `movies.cross`
- `cross/` using prefix mode → `cross/tv`, `cross/movies`

:::tip
Prefix mode with a trailing `/` creates nested categories<sup>1</sup> in qBittorrent, making it easy to group all cross-seeds under a parent category. Filtering by `cross` returns all cross-seeds (`cross/movies`, `cross/tv`, etc.).
:::

:::warning
Avoid using a leading `/` in suffix mode (e.g., `/cross-seed`). This creates the cross-seed as a **child** of the original category<sup>1</sup>, so setting your category to `movies` in Radarr would also return `movies/cross-seed` torrents, potentially causing conflicts.

Use prefix mode instead if you want nested categories.
:::

*<sup>1</sup> Nested categories require subcategories to be enabled (Instance Preferences → Files → Enable Subcategories).*

### Use indexer name as category

Sets category to the indexer name (e.g., `TorrentDB`). AutoTMM is always disabled; uses explicit save paths.

### Custom category

Uses a fixed category name for all cross-seeds (e.g., `cross-seed`). AutoTMM is always disabled; uses explicit save paths.

## Source Tagging

Configure tags applied to cross-seed torrents based on how they were discovered:

| Tag Setting | Description | Default |
|-------------|-------------|---------|
| RSS Automation Tags | Torrents added via RSS feed polling | `["cross-seed"]` |
| Seeded Search Tags | Torrents added via seeded torrent search | `["cross-seed"]` |
| Completion Search Tags | Torrents added via completion-triggered search | `["cross-seed"]` |
| Webhook Tags | Torrents added via `/apply` webhook | `["cross-seed"]` |
| Inherit source torrent tags | Also copy tags from the matched source torrent | - |

## Max Auto-Start Download

After a recheck, qui reads how much data the new cross-seed still misses. qui starts the torrent only when the missing data is at or below **Max auto-start download** (default: 50 MiB). Torrents that miss more data stay paused for manual review. Set 0 to start only fully complete torrents.

When only ignorable files are missing (samples, `.nfo`, subtitles, and similar sidecar files), qui starts the torrent anyway. This exception has a fixed 200 MiB ceiling.

This limit applies to new cross-seed additions from RSS, seeded search, completion search, and the webhooks. The season-pack flow and Dir Scan use their own resume rules and are not affected.

## External Program

Optionally run an external program after successfully injecting a cross-seed torrent.

## Category Behavior Details

### autoTMM (Auto Torrent Management)

autoTMM behavior depends on which category mode is active:

| Category Mode | autoTMM Behavior |
|---------------|------------------|
| **Category Affix** | Inherited from matched torrent (regular mode only; hardlink/reflink disables autoTMM) |
| **Indexer name** | Always disabled (explicit save paths) |
| **Custom** | Always disabled (explicit save paths) |

When autoTMM is inherited (affix mode):
- If matched torrent uses autoTMM, cross-seed uses autoTMM
- If matched torrent has manual path, cross-seed uses same manual path

When autoTMM is disabled (indexer/custom modes), cross-seeds always use explicit save paths derived from the matched torrent's location.

:::note
Hardlink/reflink mode always adds torrents with an explicit `savepath` pointing at the link tree, which forces autoTMM off.
Dir Scan injections are separate from cross-seed rules and also always add with explicit `savepath` (autoTMM off).
:::

### Save Path Determination

Priority order:
1. Base category's explicit save path (if configured in qBittorrent)
2. Matched torrent's current save path (fallback)

**Examples:**

*Suffix mode (default):*
- `tv` category has save path `/data/tv`
- Cross-seed gets `tv.cross` category with save path `/data/tv`
- Files are found because they're in the same location

*Prefix mode:*
- `movies` category has save path `/data/movies`
- Cross-seed gets `cross/movies` category with save path `/data/movies`
- Nested `cross/` parent in qBittorrent groups all cross-seeds together

## Best Practices

**Do:**
- Use autoTMM consistently across your torrents
- Let qui create cross-seed categories automatically
- Keep category structures simple
- Use prefix mode with `/` (e.g., `cross/`) if you want all cross-seeds grouped under one parent category

**Don't:**
- Manually move torrent files after adding them
- Create cross-seed categories manually with different paths
- Mix autoTMM and manual paths for the same content type
