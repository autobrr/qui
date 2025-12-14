# Cross-Seeding in qui

## How It Works

When you cross-seed a torrent, qui:
1. Finds a matching torrent in your library (same content, different tracker)
2. Adds the new torrent pointing to your existing files
3. Applies the correct category and save path automatically

## Category Behavior

### The .cross Suffix

When enabled, cross-seeded torrents get a `.cross` suffix on their category:
- Original torrent: `tv` category
- Cross-seed: `tv.cross` category

**Why?** This prevents *arr applications (Sonarr, Radarr, etc.) from seeing cross-seeded torrents, avoiding duplicate import attempts.

The `.cross` category is created automatically with the same save path as the base category.

### autoTMM (Auto Torrent Management)

Cross-seeds inherit the autoTMM setting from the matched torrent:
- If matched torrent uses autoTMM -> cross-seed uses autoTMM
- If matched torrent has manual path -> cross-seed uses same manual path

This ensures files are always saved to the correct location.

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

### "Files not found" after cross-seed

- Check that the cross-seed's save path matches where files actually exist
- Verify the matched torrent's save path in qBittorrent
- Ensure the matched torrent has completed downloading (100% progress)

### Cross-seed in wrong category

- Check your cross-seed settings in qui
- Verify the matched torrent has the expected category

### autoTMM unexpectedly enabled/disabled

- This mirrors the matched torrent's setting - it's intentional
- Check the original torrent's autoTMM status in qBittorrent
