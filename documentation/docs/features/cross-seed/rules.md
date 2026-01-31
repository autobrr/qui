---
sidebar_position: 2
title: Rules
---

# Cross-Seed Rules

Configure matching behavior in the **Rules** tab on the Cross-Seed page.

## Matching

- **Find individual episodes** - When enabled, season packs also match individual episodes. When disabled, season packs only match other season packs. Episodes are added with AutoTMM disabled to prevent save path conflicts.
- **Size mismatch tolerance** - Maximum size difference percentage (default: 5%). Also determines auto-resume threshold after recheck.
- **Skip recheck** - When enabled, skips any cross-seed that would require a recheck (alignment needed, extra files, or disc layouts like `BDMV`/`VIDEO_TS`). Applies to all modes including hardlink/reflink.
- **Skip piece boundary safety check** - Enabled by default. When enabled, allows cross-seeds even if extra files share torrent pieces with content files. **Warning:** This may corrupt your existing seeded data if content differs. Uncheck this to enable the safety check, or use reflink mode which safely handles these cases.

:::note
Disc layouts (`BDMV`/`VIDEO_TS`) are treated more strictly: they only auto-resume after a full recheck reaches 100%.
:::

## Categories

Choose one of three mutually exclusive category modes:

### Category Affix (default)

Adds a configurable affix to the matched torrent's category. Prevents Sonarr/Radarr from importing cross-seeded files as duplicates. AutoTMM is inherited from the matched torrent.

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

## External Program

Optionally run an external program after successfully injecting a cross-seed torrent.

## Category Behavior Details

### autoTMM (Auto Torrent Management)

autoTMM behavior depends on which category mode is active:

| Category Mode | autoTMM Behavior |
|---------------|------------------|
| **Category Affix** | Inherited from matched torrent |
| **Indexer name** | Always disabled (explicit save paths) |
| **Custom** | Always disabled (explicit save paths) |

When autoTMM is inherited (affix mode):
- If matched torrent uses autoTMM, cross-seed uses autoTMM
- If matched torrent has manual path, cross-seed uses same manual path

When autoTMM is disabled (indexer/custom modes), cross-seeds always use explicit save paths derived from the matched torrent's location.

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
