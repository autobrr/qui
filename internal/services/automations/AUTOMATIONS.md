# Automations

Automations automatically apply speed limits, ratio caps, and seeding time limits to torrents based on their tracker domain.

## How Automations Work

Automations are evaluated in **sort order** (first match wins). Each automation can match torrents by:

1. **Tracker domain** (required) - The tracker's hostname
2. **Category** (optional) - The torrent's category in qBittorrent
3. **Tag** (optional) - A tag assigned to the torrent

Torrents that don't match any automation are left untouched. Disabled automations are skipped entirely.

## Settings Applied

When an automation matches a torrent, it can apply any combination of:

| Setting | Description |
|---------|-------------|
| Upload limit | Maximum upload speed (KiB/s) |
| Download limit | Maximum download speed (KiB/s) |
| Ratio limit | Stop seeding when this ratio is reached |
| Seeding time limit | Stop seeding after this many minutes |

## When Automations Run

Automations are applied in two ways:

- **Automatically** - A background service scans all torrents every 15 minutes
- **Manually** - Click "Apply Now" in the UI to trigger immediately

To avoid hammering qBittorrent, the same torrent won't be re-processed within 2 minutes (debouncing).

## Matching Logic

### Domain Patterns

Tracker domains can be matched in three ways:

| Pattern | Example | Matches |
|---------|---------|---------|
| Exact | `tracker.example.com` | Only `tracker.example.com` |
| Glob | `*.example.com` | `sub.example.com`, `tracker.example.com` |
| Suffix | `.example.com` | `example.com`, `sub.example.com` |

### Multiple Patterns

Separate multiple patterns with commas, semicolons, or pipes:

```text
tracker1.com,tracker2.org|tracker3.net
```

All matching is **case-insensitive**.

## Important Behavior

### Automations Only Set Values

Automations apply settings to torrents - they **do not revert** settings when the automation is disabled or deleted. If you disable an automation that set upload limit to 1000 KiB/s, affected torrents keep that limit until you manually change it or another automation applies a different value.

### Efficient Updates

The service only sends API calls to qBittorrent when the torrent's current setting differs from what the automation specifies. If a torrent already has the correct limits, it's skipped.

### Existing vs New Torrents

- **Existing torrents** - Processed on the next scan cycle (within 15 minutes)
- **New torrents** - Picked up automatically within 15 minutes of appearing in qBittorrent

### Batched API Calls

To handle large torrent collections efficiently, torrents are grouped by setting value and sent to qBittorrent in batches of up to 150 hashes per API call.

## Age Fields

In addition to absolute timestamp fields (`Added On`, `Completed On`, `Last Activity`), the query builder supports **age** versions of these fields that express time as "how long ago" rather than as a specific date:

| Field | Description | Example |
|-------|-------------|---------|
| Added Age | Time since torrent was added | `Added Age > 7 days` - matches torrents added more than 7 days ago |
| Completed Age | Time since download completed | `Completed Age < 1 hour` - matches torrents completed within the last hour |
| Inactive Time | Time since last activity | `Inactive Time > 24 hours` - matches inactive torrents |

### When to Use Age Fields

Age fields are useful when you want rules that adapt to the current time:

- **"Delete torrents older than 30 days"** - Use `Added Age > 30 days`
- **"Pause torrents with no activity for 1 week"** - Use `Inactive Time > 7 days`
- **"Apply limits to recently completed torrents"** - Use `Completed Age < 2 hours`

### Behavior Notes

- **Unset timestamps**: If completion or last activity is 0 (never completed / never had activity), the age condition will **not match**. This prevents unexpected matches on torrents still downloading.
- **Clock skew**: Negative ages (timestamp in the future) are clamped to 0 seconds to avoid unexpected behavior from clock drift.
- **Input format**: Age values use the same duration input as Seeding Time (minutes, hours, or days).
