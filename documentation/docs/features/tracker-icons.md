---
sidebar_position: 11
title: Tracker Icons
description: Automatic favicon caching for tracker hosts.
---

# Tracker icons

qui stores cached icons in your data directory under `tracker-icons/`. If you use the default SQLite engine, this directory sits next to `qui.db`. If you use Postgres, qui uses the same data directory. qui stores icons as 16×16 PNGs and rejects source images larger than 1024×1024.

qui downloads a favicon the first time it sees a tracker host and caches it for future sessions. If a download fails, qui waits 30 minutes before it tries that host again.

If you want to disable these network fetches, set `trackerIconsFetchEnabled = false` in `config.toml` (or `QUI__TRACKER_ICONS_FETCH_ENABLED=false`).

## Add icons manually

Copy PNGs named after each tracker host (for example, `tracker.example.com.png`) into the `tracker-icons/` directory. qui serves the files as-is, so trim and resize them yourself. If icons match the built-in size (16×16), they stay crisp and avoid extra scaling.

## Preload a bundle of icons

If you have a library of icons, preload them with a mapping file in `tracker-icons/`. qui accepts the first of these file names that it finds: `preload.json`, `preload.js`, `tracker-icons.json`, `tracker-icons.js`, `tracker-icons.txt`.

### Format

Use a plain JSON object or export a snippet as `const trackerIcons = { ... };`.

- Keys must be the real tracker hostnames (for example, `tracker.example.org`)
- If you include a `www.*` host and the bare hostname lacks an icon, qui mirrors the icon to the bare hostname
- On startup, qui decodes each data URL, normalizes the image to 16×16, and writes the PNG to `<host>.png`
- qui skips a host that already has a `<host>.png`. To make the preload write it again, delete the old file

### JSON example

```json
{
  "tracker.example.org": "data:image/png;base64,AAA...",
  "www.tracker.org": "data:image/png;base64,BBB..."
}
```

### JavaScript example

```js
const trackerIcons = {
  "tracker.example.org": "data:image/png;base64,CCC...",
  "www.tracker.org": "data:image/png;base64,DDD..."
};
```

### Community resources

See [Audionut/add-trackers](https://github.com/Audionut/add-trackers/blob/8db05c0e822f9b3afa46ca784644c4e7e400c92b/ptp-add-filter-all-releases-anut.js#L768) for an example icon bundle.
