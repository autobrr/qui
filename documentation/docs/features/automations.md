---
sidebar_position: 2
title: Automations
description: Rule-based automation for torrent management.
---

# Automations

Automations are a rule-based engine that automatically applies actions to torrents based on conditions. Use them to manage speed limits, delete old torrents, organize with tags and categories, and more.

## How Automations Work

Automations are evaluated in **sort order** (first match wins for exclusive actions like delete). Each rule can match torrents using a flexible query builder with nested conditions.

- **Automatic** - Background service scans torrents every 20 seconds
- **Per-Rule Intervals** - Each rule can have its own interval (minimum 60 seconds, default 15 minutes)
- **Manual** - Click "Apply Now" to trigger immediately (bypasses interval checks)
- **Debouncing** - Same torrent won't be re-processed within 2 minutes

## Query Builder

The query builder supports complex nested conditions with AND/OR groups. Drag conditions to reorder them.

### Available Condition Fields

#### Identity Fields
| Field | Description |
|-------|-------------|
| Name | Torrent display name (supports cross-category operators) |
| Hash | Info hash |
| Category | qBittorrent category |
| Tags | Set-based tag matching |
| State | Status filter (see State Values below) |

#### Path Fields
| Field | Description |
|-------|-------------|
| Save Path | Download location |
| Content Path | Full path to content |

#### Size Fields (bytes)
| Field | Description |
|-------|-------------|
| Size | Selected file size |
| Total Size | Total torrent size |
| Downloaded | Bytes downloaded |
| Uploaded | Bytes uploaded |
| Amount Left | Remaining bytes |
| Free Space | Free space on the instance's filesystem (updates on Delete, oldest first) |

#### Time Fields
| Field | Description |
|-------|-------------|
| Seeding Time | Time spent seeding (seconds) |
| Time Active | Total active time (seconds) |
| Added On Age | Time since added |
| Completion On Age | Time since completed |
| Last Activity Age | Time since last activity |

#### Progress Fields
| Field | Description |
|-------|-------------|
| Ratio | Upload/download ratio |
| Progress | Download progress (0-1) |
| Availability | Distributed copies available |

#### Speed Fields (bytes/s)
| Field | Description |
|-------|-------------|
| Download Speed | Current download speed |
| Upload Speed | Current upload speed |

#### Peer Fields
| Field | Description |
|-------|-------------|
| Active Seeders | Currently connected seeders |
| Active Leechers | Currently connected leechers |
| Total Seeders | Tracker-reported seeders |
| Total Leechers | Tracker-reported leechers |
| Trackers Count | Number of trackers |

#### Tracker/Status Fields
| Field | Description |
|-------|-------------|
| Tracker | Primary tracker URL |
| Private | Boolean - is private tracker |
| Is Unregistered | Boolean - tracker reports unregistered |
| Comment | Torrent comment field |

#### Advanced Fields
| Field | Description |
|-------|-------------|
| Hardlink Scope | `none`, `torrents_only`, or `outside_qbittorrent` (requires local filesystem access) |

### State Values

The State field matches these status buckets:

| State | Description |
|-------|-------------|
| `downloading` | Actively downloading |
| `uploading` | Actively uploading |
| `completed` | Download finished |
| `stopped` | Paused by user |
| `active` | Has transfer activity |
| `inactive` | No current activity |
| `running` | Not paused |
| `stalled` | No peers available |
| `errored` | Has errors |
| `tracker_down` | Tracker unreachable |
| `checking` | Verifying files |
| `moving` | Moving files |
| `missingFiles` | Files not found |
| `unregistered` | Tracker reports unregistered |

### Operators

**String:** equals, not equals, contains, not contains, starts with, ends with, matches regex

**Numeric:** `=`, `!=`, `>`, `>=`, `<`, `<=`, between

**Boolean:** is, is not

**State:** is, is not

**Cross-Category (Name field only):**
- `EXISTS_IN` - Exact name match in target category
- `CONTAINS_IN` - Partial/normalized name match in target category

### Regex Support

Full RE2 (Go regex) syntax supported. Patterns are case-insensitive by default. The UI validates patterns and shows helpful error messages for invalid regex.

## Tracker Matching

This is sort of not needed, since you can already scope trackers outside the workflows. But its available either way.

| Pattern | Example | Matches |
|---------|---------|---------|
| All | `*` | Every tracker |
| Exact | `tracker.example.com` | Only that domain |
| Glob | `*.example.com` | Subdomains |
| Suffix | `.example.com` | Domain and subdomains |

Separate multiple patterns with commas, semicolons, or pipes. All matching is case-insensitive.

## Actions

Actions can be combined (except Delete which must be standalone). Each action supports an optional condition override.

### Speed Limits

Set upload and/or download limits in KiB/s. Applied in batches for efficiency.

### Share Limits

Set ratio limit and/or seeding time limit (minutes). Torrents stop seeding when limits are reached.

### Pause

Pause matching torrents. Only pauses if not already stopped.

### Delete

Remove torrents from qBittorrent. **Must be standalone** - cannot combine with other actions.

| Mode | Description |
|------|-------------|
| `delete` | Remove from client, keep files |
| `deleteWithFiles` | Remove with files |
| `deleteWithFilesPreserveCrossSeeds` | Remove files but preserve if cross-seeds detected |

### Tag

Add or remove tags from torrents.

| Mode | Description |
|------|-------------|
| `full` | Add to matches, remove from non-matches (smart toggle) |
| `add` | Only add to matches |
| `remove` | Only remove from non-matches |

Options:
- **Use Tracker as Tag** - Derive tag from tracker domain
- **Use Display Name** - Use tracker customization display name instead of raw domain

### Category

Move torrents to a different category.

Options:
- **Include Cross-Seeds** - Also move cross-seeds (matching ContentPath AND SavePath)
- **Block If Cross-Seed In Categories** - Prevent move if another cross-seed is in protected categories

## Cross-Seed Awareness

Automations detect cross-seeded torrents (same content/files) and can handle them specially:

- **Detection** - Matches both ContentPath AND SavePath
- **Delete Rules** - Use `deleteWithFilesPreserveCrossSeeds` to keep files if cross-seeds exist
- **Category Rules** - Enable "Include Cross-Seeds" to move related torrents together
- **Blocking** - Prevent category moves if cross-seeds are in protected categories

## Hardlink Detection

The `Hardlink Scope` field detects whether torrent files have hardlinks:

| Value | Description |
|-------|-------------|
| `none` | No hardlinks detected |
| `torrents_only` | Hardlinks only within qBittorrent's download set |
| `outside_qbittorrent` | Hardlinks to files outside qBittorrent (e.g., media library) |

:::note
Requires "Local filesystem access" enabled on the instance.
:::

Use case: Identify library imports vs pure cross-seeds for selective cleanup.

## Important Behavior

### Settings Only Set Values

Automations apply settings but **do not revert** when disabled or deleted. If a rule sets upload limit to 1000 KiB/s, affected torrents keep that limit until manually changed or another rule applies a different value.

### Efficient Updates

Only sends API calls when the torrent's current setting differs from the desired value. No-op updates are skipped.

### Processing Order

- **First match wins** for exclusive actions (delete, category)
- **Accumulative** for combinable actions (tags, speed limits)
- Delete ends torrent processing (no further rules evaluated)

### Free Space Condition Behavior

When using the **Free Space** condition in delete rules, the system uses intelligent cumulative tracking:

1. **Oldest-first processing** - Torrents are sorted by age (oldest first) for deterministic, predictable cleanup
2. **Cumulative space tracking** - As each torrent is marked for deletion, its size is added to the projected free space
3. **Stop when satisfied** - Once `Free Space + Space To Be Cleared` exceeds your threshold, remaining torrents no longer match
4. **Cross-seed aware** - Cross-seeded torrents sharing the same files are only counted once to avoid overestimating freed space

**Example:** With 400GB free and a rule "Delete if Free Space < 500GB", the system deletes oldest torrents until the cumulative freed space reaches 100GB, then stops. A 50GB torrent and its cross-seed (same files) only count as 50GB freed, not 100GB.

### Batching

Torrents are grouped by action value and sent to qBittorrent in batches of up to 50 hashes per API call.

## Activity Log

All automation actions are logged with:
- Torrent name and hash
- Rule name and action type
- Outcome (success/failed) with reasons
- Action-specific details

Activity is retained for 7 days by default. View the log in the Automations section for each instance.

## Example Rules

### Delete Old Completed Torrents if low on disk space

Match torrents completed over 30 days ago when filesystem is lower than 500GB:
- Condition: `Completion On Age > 30 days` AND `State is completed` AND `Free Space < 500GB`
- Action: Delete with files

The system deletes oldest matching torrents first, stopping once enough space would be freed to exceed 500GB. This prevents over-deletion when you only need to clear a small amount of space.

### Speed Limit Private Trackers

Limit upload on private trackers:
- Tracker: `*`
- Condition: `Private is true`
- Action: Upload limit 10000 KiB/s

### Tag Stalled Torrents

Auto-tag torrents with no activity:
- Tracker: `*`
- Condition: `Last Activity Age > 7 days`
- Action: Tag "stalled" (mode: add)

### Clean Unregistered Torrents

Remove torrents the tracker no longer recognizes:
- Tracker: `*`
- Condition: `Is Unregistered is true`
- Action: Delete (keep files)

### Maintain Minimum Free Space

Keep at least 200GB free by removing oldest completed torrents:
- Tracker: `*`
- Condition: `Free Space < 200GB` AND `State is completed`
- Action: Delete with files (preserve cross-seeds)

Only deletes the minimum number of torrents needed to reach 200GB free. Oldest torrents are removed first, and cross-seeded content is preserved if other torrents reference the same files.

### Organize by Tracker

Move torrents to tracker-named categories:
- Tracker: `tracker.example.com`
- Action: Category "example" with "Include Cross-Seeds" enabled
