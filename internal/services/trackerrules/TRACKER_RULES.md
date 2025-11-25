# Tracker Rules

Tracker Rules automatically apply speed limits, ratio caps, and seeding time limits to torrents based on their tracker domain.

## How Rules Work

Rules are evaluated in **sort order** (first match wins). Each rule can match torrents by:

1. **Tracker domain** (required) - The tracker's hostname
2. **Category** (optional) - The torrent's category in qBittorrent
3. **Tag** (optional) - A tag assigned to the torrent

Torrents that don't match any rule are left untouched. Disabled rules are skipped entirely.

## Settings Applied

When a rule matches a torrent, it can apply any combination of:

| Setting | Description |
|---------|-------------|
| Upload limit | Maximum upload speed (KiB/s) |
| Download limit | Maximum download speed (KiB/s) |
| Ratio limit | Stop seeding when this ratio is reached |
| Seeding time limit | Stop seeding after this many minutes |

## When Rules Run

Rules are applied in two ways:

- **Automatically** - A background service scans all torrents every 20 seconds
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

```
tracker1.com,tracker2.org|tracker3.net
```

All matching is **case-insensitive**.

## Important Behavior

### Rules Only Set Values

Rules apply settings to torrents - they **do not revert** settings when the rule is disabled or deleted. If you disable a rule that set upload limit to 1000 KiB/s, affected torrents keep that limit until you manually change it or another rule applies a different value.

### Efficient Updates

The service only sends API calls to qBittorrent when the torrent's current setting differs from what the rule specifies. If a torrent already has the correct limits, it's skipped.

### Existing vs New Torrents

- **Existing torrents** - Processed on the next scan cycle (within 20 seconds)
- **New torrents** - Picked up automatically within 20 seconds of appearing in qBittorrent

### Batched API Calls

To handle large torrent collections efficiently, torrents are grouped by setting value and sent to qBittorrent in batches of up to 150 hashes per API call.
