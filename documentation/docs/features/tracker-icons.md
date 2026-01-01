---
sidebar_position: 8
---

# Tracker Icons

Cached icons live in your data directory under `tracker-icons/` (next to `qui.db`). Icons are stored as normalised 16×16 PNGs; anything larger than 1024×1024 is rejected, so resize first if you are supplying files manually.

qui automatically attempts to download a favicon the first time it encounters a tracker host, caching the result for future sessions. After a failed download it waits 30 minutes before retrying the same host, and the next retry is triggered automatically the next time that host appears in your tracker list.

Set `trackerIconsFetchEnabled = false` in `config.toml` (or `QUI__TRACKER_ICONS_FETCH_ENABLED=false`) to disable these network fetches.

## Add Icons Manually

Copy PNGs named after each tracker host (e.g. `tracker.example.com.png`) into the `tracker-icons/` directory. Files are served as-is, so trimming or resizing is up to you, but matching the built-in size (16×16) keeps them crisp and avoids extra scaling.

## Preload a Bundle of Icons

If you already have a library of icons (for example, exported from another installation) you can preload them via a mapping file placed alongside the directory: `tracker-icons/preload.json`, `tracker-icons/preload.js`, `tracker-icons/tracker-icons.json`, `tracker-icons/tracker-icons.js`, or `tracker-icons/tracker-icons.txt`.

### Format

The file can be either a plain JSON object or a snippet exported as `const trackerIcons = { ... };`.

- Keys must be the real tracker hostnames (e.g. `tracker.example.org`)
- If you include a `www.*` host, qui automatically mirrors the icon to the bare hostname when missing
- On startup qui decodes each data URL, normalises the image to 16×16, and writes the PNG to `<host>.png`

### JSON Example

```json
{
  "tracker.example.org": "data:image/png;base64,AAA...",
  "www.tracker.org": "data:image/png;base64,BBB..."
}
```

### JavaScript Example

```js
const trackerIcons = {
  "tracker.example.org": "data:image/png;base64,CCC...",
  "www.tracker.org": "data:image/png;base64,DDD..."
};
```

### Community Resources

See [Audionut/add-trackers](https://github.com/Audionut/add-trackers/blob/8db05c0e822f9b3afa46ca784644c4e7e400c92b/ptp-add-filter-all-releases-anut.js#L768) for an example icon bundle.
