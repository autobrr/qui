---
sidebar_position: 2
---

# Cross-Seed Rules

Configure matching behavior in the **Rules** tab on the Cross-Seed page.

## Matching

- **Find individual episodes** - When enabled, season packs also match individual episodes. When disabled, season packs only match other season packs. Episodes are added with AutoTMM disabled to prevent save path conflicts.
- **Size mismatch tolerance** - Maximum size difference percentage (default: 5%). Also determines auto-resume threshold after recheck.
- **Skip piece boundary safety check** - Enabled by default. When enabled, allows cross-seeds even if extra files share torrent pieces with content files. **Warning:** This may corrupt your existing seeded data if content differs. Uncheck this to enable the safety check, or use reflink mode which safely handles these cases.

## Categories

Choose one of three mutually exclusive category modes:

### Add .cross category suffix (default)

Appends `.cross` to cross-seed categories (e.g., `movies` → `movies.cross`). Prevents Sonarr/Radarr from importing cross-seeded files as duplicates. AutoTMM is inherited from the matched torrent.

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

## Allowed Extra Files

File patterns excluded from comparison when matching torrents. Adding patterns here **increases matches** by allowing torrents to match even if they differ in these files (e.g., one has an NFO, the other doesn't).

- Plain strings match any path ending in the text (e.g., `.nfo` matches all `.nfo` files)
- Glob patterns treat `/` as a folder separator (e.g., `*/*sample/*` matches sample folders)

:::note
These patterns only apply to reuse mode. Hardlink mode requires a 1:1 file match and won't download extras—if the incoming torrent has files not present in the matched torrent, hardlink mode fails.
:::

## External Program

Optionally run an external program after successfully injecting a cross-seed torrent.

## Category Behavior Details

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

### Save Path Determination

Priority order:
1. Base category's explicit save path (if configured in qBittorrent)
2. Matched torrent's current save path (fallback)

**Example:**
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
