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
| Free Space | Free space on disk (configurable source - see [Free Space Source](#free-space-source)) |

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
| Progress | Download progress (0-100%) |
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
| Has Missing Files | Boolean - completed torrent has files missing on disk (requires local filesystem access) |

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

There are two ways to use regex in filter conditions:

**The `matches regex` operator** is a dedicated operator where the value is always treated as a regex pattern. The condition is true if the pattern matches anywhere in the field value.

**The regex toggle (`.*` button)** appears next to the value input on other string operators such as `equals`, `contains`, `not contains`, `starts with`, and `ends with`. When enabled, the value is treated as a regex pattern.

:::warning Regex toggle overrides the selected operator

When the regex toggle is enabled, the selected operator's logic (negation, containment, prefix/suffix matching) is **not applied**. The condition becomes a simple regex match, equivalent to `matches regex`, regardless of which operator is selected in the dropdown.

This means `not contains` with the regex toggle enabled does **not** negate the match. It behaves the same as `matches regex` -- if the pattern is found, the condition evaluates to true.

To negate a regex match, use the **NOT toggle** (the `IF / IF NOT` button at the start of the condition row) together with the `matches regex` operator.
:::

Full RE2 (Go regex) syntax is supported. Patterns are case-insensitive by default. The UI validates patterns and shows helpful error messages for invalid regex.

#### Tags and regex

Without the regex toggle, tag operators like `contains` and `not contains` check each tag individually (set-based matching). With regex enabled (or when using `matches regex`), the pattern is matched against the full raw comma-separated tag string (e.g., `cross-seed, noHL, racing`).

**Example: exclude torrents tagged with `tag1` or `tag2`**

| Setting | Value |
|---|---|
| Field | Tags |
| Toggle | `IF NOT` |
| Operator | `matches regex` |
| Value | `tag1|tag2` |

This evaluates the regex `tag1|tag2` against the raw tags string. The `NOT` toggle then negates the result, so the condition is true only for torrents that do **not** have either tag.
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

Set upload and/or download limits. Each field supports these modes:

| Mode | Value | Description |
|------|-------|-------------|
| No change | - | Don't modify this field |
| Unlimited | 0 | Remove speed limit (qBittorrent treats 0 as unlimited) |
| Custom | >0 | Specific limit in KiB/s or MiB/s |

Applied in batches for efficiency.

### Share Limits

Set ratio limit and/or seeding time limit. Each field supports these modes:

| Mode | Value | Description |
|------|-------|-------------|
| No change | - | Don't modify this field |
| Use global | -2 | Follow qBittorrent's global share settings |
| Unlimited | -1 | No limit for this field |
| Custom | >=0 | Specific value (ratio as decimal, time in minutes) |

Torrents stop seeding when any enabled limit is reached.

### Pause

Pause matching torrents. Only pauses if not already stopped.

### Delete

Remove torrents from qBittorrent. **Must be standalone** - cannot combine with other actions.

| Mode | Description |
|------|-------------|
| `delete` | Remove from client, keep files |
| `deleteWithFiles` | Remove with files |
| `deleteWithFilesPreserveCrossSeeds` | Remove files but preserve if cross-seeds detected |
| `deleteWithFilesIncludeCrossSeeds` | Remove files and also delete all cross-seeded torrents sharing the same files |

**Include cross-seeds mode behavior:**

When a torrent matches the rule, the system finds other torrents that point to the same downloaded files (cross-seeds/duplicates) and deletes them together. This is useful when you want to fully remove content and all its cross-seeded copies at once.

- **Safe expansion**: If qui can't safely confirm another torrent uses the same files, it won't be included in the deletion.
- **Safety-first**: If verification can't complete for any reason, the entire group is skipped rather than risking broken torrents.
- **Preview**: The delete preview shows all torrents that would be deleted, with cross-seeds marked.

**Include hardlinked copies:**

When "Include hardlinked copies" is enabled (only available with `deleteWithFilesIncludeCrossSeeds` mode), the system also deletes torrents that share the same underlying physical files via hardlinks, even if they have different Content Paths.

- **Requires**: Local Filesystem Access must be enabled on the instance.
- **Safe scope**: Only includes hardlinks that are fully contained within qBittorrent's torrent set. Never follows hardlinks to files outside qBittorrent (e.g., your media library).
- **Preview**: Hardlink-expanded torrents are marked as "Cross-seed (hardlinked)" in the preview.
- **Free Space projection**: When combined with Free Space conditions, hardlink groups are correctly deduplicated in the space projection - torrents sharing the same physical files are only counted once.

This is useful when you have hardlinked copies of content across different locations in qBittorrent and want to clean up all copies together.

### Tag

Add or remove tags from torrents.

| Mode | Description |
|------|-------------|
| `full` | Add to matches, remove from non-matches (smart toggle) |
| `add` | Only add to matches |
| `remove` | Only remove from non-matches |

:::note
Mode does not change the way torrents are flagged, meaning, even with `mode: remove`, tags will be removed if the torrent does **NOT** match the conditions. `mode: remove` simply means that tags will not be added to torrents that do match.
:::

Options:
- **Use Tracker as Tag** - Derive tag from tracker domain
- **Use Display Name** - Use tracker customization display name instead of raw domain

### Category

Move torrents to a different category.

Options:
- **Include Cross-Seeds** - Also move cross-seeds (matching ContentPath AND SavePath)
- **Block If Cross-Seed In Categories** - Prevent move if another cross-seed is in protected categories

### Move

Move torrents to a different path on disk. This is needed to move the contents if AutoTMM is not enabled.

Options:
- **Skip if cross-seeds don't match the rule's conditions** - Skip the move if the torrent has cross-seeds that don't match the rule's conditions

## Cross-Seed Awareness

Automations detect cross-seeded torrents (same content/files) and can handle them specially:

- **Detection** - Matches via ContentPath (and SavePath for category moves)
- **Delete Rules**:
  - Use `deleteWithFilesPreserveCrossSeeds` to keep files if cross-seeds exist
  - Use `deleteWithFilesIncludeCrossSeeds` to delete matching torrents and all their cross-seeds together
- **Category Rules** - Enable "Include Cross-Seeds" to move related torrents together
- **Blocking** - Prevent category moves if cross-seeds are in protected categories

### Hardlink Detection

The `HARDLINK_SCOPE` field lets automations distinguish between torrents whose files are hardlinked into an external library (Sonarr, Radarr, etc.) and torrents that exist only within qBittorrent. This is the foundation for safe "Remove Upgraded Torrents" automations.

#### How scope is determined

When an automation references `HARDLINK_SCOPE`, qui builds a hardlink index by calling `Lstat()` on every file of every torrent in qBittorrent. For each file it extracts:

- The **inode** and **device ID** — uniquely identifying the file on disk.
- The **nlink count** — the total number of hardlinks to that inode, as reported by the filesystem.

It then counts how many unique file paths across the entire qBittorrent torrent set point to each inode. The scope for each torrent is determined by comparing these two numbers:

| Scope | Condition | Meaning |
|---|---|---|
| `none` | No file has `nlink > 1` | No hardlinks detected. |
| `torrents_only` | At least one file has `nlink > 1`, and no file has `nlink > uniquePathCount` | Hardlinks exist, but only between torrents in qBittorrent. No external library links. |
| `outside_qbittorrent` | Any file has `nlink > uniquePathCount` | Something outside qBittorrent has hardlinked the file — typically a Sonarr/Radarr library import. |

:::note
`HARDLINK_SCOPE` only reflects hardlink metadata. Cross-seeds are detected separately (ContentPath matching), so a torrent can have `HARDLINK_SCOPE = none` and still be cross-seeded.
:::

#### Unknown scope and safety behavior

If qui cannot `Lstat()` **any** file in a torrent — due to wrong paths, missing permissions, or inaccessible storage — that torrent receives no scope entry. All `HARDLINK_SCOPE` conditions evaluate to `false` for that torrent, regardless of the operator or value. This is a safety measure to prevent unintended deletions of torrents qui cannot fully inspect.

To diagnose this, enable debug logging and look for the "hardlink index built" log message, which reports an `inaccessible` count.

#### Docker volume requirements

For hardlink scope detection to work in Docker:

1. **Paths must match exactly.** qui must be able to read files at the same paths qBittorrent reports. If qBittorrent says a torrent's save path is `/data/torrents/radarr/`, qui must be able to access `/data/torrents/radarr/` inside its container.

2. **Same underlying storage.** Both containers must share the same host mount so that inode numbers are consistent. If qui and qBittorrent access the same files through different host mounts or different bind-mount configurations, inode numbers may not match.

3. **Single mount, not subdivided.** Mount the common parent directory rather than mounting subdirectories separately. For example, if your data lives under `/mnt/media/data` on the host:

```yaml
services:
  qui:
    volumes:
      - /home/user/docker/qui:/config
      - /mnt/media/data:/data  # single mount covering both torrents and library
```

Avoid mounting both `/mnt/media/data/torrents:/data/torrents` **and** `/mnt/media/data:/data` — the overlapping mounts can cause inconsistent inode visibility. Use a single mount at the common parent.

#### Filesystem limitations

Hardlink scope detection depends on the kernel reporting accurate `nlink` values in stat results. Some filesystems do not do this:

- **FUSE-based filesystems** (sshfs, mergerfs, rclone mount) may report `nlink = 1` for all files regardless of actual hardlink count.
- **Some NAS appliance filesystems** and **overlay filesystems** (overlayfs) may behave similarly.
- **Network filesystems** (NFS, CIFS/SMB) generally report accurate nlink values but behavior varies by server implementation.

On affected filesystems, every torrent appears to have scope `none` because nlink is always 1. There is no workaround within qui — this is a kernel/filesystem limitation. If you suspect this issue, run `stat` on a file you know is hardlinked and check the "Links" count.

Hardlinks also cannot span across different filesystems. If your torrent data and media library are on separate filesystems (or separate Docker volumes backed by different host paths), Sonarr/Radarr will copy instead of hardlink, and scope detection has nothing to detect.

#### Example: Remove Upgraded Torrents

This automation deletes torrents that have been replaced by an upgrade in Sonarr/Radarr. It targets torrents where the library hardlink no longer exists (the arr removed or re-linked it during upgrade), the torrent has been seeding for at least 7 days, and the category matches your arr categories.

:::tip
Use `HARDLINK_SCOPE` with `NOT_EQUAL` to `outside_qbittorrent` rather than `EQUAL` to `none`. This way torrents with scope `torrents_only` (cross-seeded but not in a library) are also eligible for cleanup, while any torrent still linked into your media library is protected.
:::

```json
{
  "name": "Remove Upgraded Torrents",
  "trackerPattern": "*",
  "trackerDomains": ["*"],
  "conditions": {
    "schemaVersion": "1",
    "delete": {
      "enabled": true,
      "mode": "deleteWithFilesPreserveCrossSeeds",
      "condition": {
        "operator": "AND",
        "conditions": [
          {
            "operator": "OR",
            "conditions": [
              { "field": "CATEGORY", "operator": "EQUAL", "value": "radarr" },
              { "field": "CATEGORY", "operator": "EQUAL", "value": "radarr.cross" },
              { "field": "CATEGORY", "operator": "EQUAL", "value": "tv-sonarr" },
              { "field": "CATEGORY", "operator": "EQUAL", "value": "tv-sonarr.cross" }
            ]
          },
          {
            "field": "HARDLINK_SCOPE",
            "operator": "NOT_EQUAL",
            "value": "outside_qbittorrent"
          },
          {
            "field": "SEEDING_TIME",
            "operator": "GREATER_THAN_OR_EQUAL",
            "value": "604800"
          }
        ]
      }
    }
  }
}
```

This works because when Sonarr/Radarr upgrades a release, the old library hardlink is removed. The old torrent's files then have `nlink == 1` (scope `none`) or are only linked to other torrents (scope `torrents_only`). Either way, the scope is not `outside_qbittorrent`, so the automation matches and deletes the torrent after the seeding time requirement is met.

If the automation is matching torrents you expect to be protected, verify:

1. qui can access all torrent files at the paths qBittorrent reports (check debug logs for inaccessible files).
2. Your filesystem reports accurate nlink values (`stat <file>` should show Links > 1 for hardlinked files).
3. Your Docker volume mounts do not overlap or subdivide the storage in a way that breaks inode consistency.
## Missing Files Detection

The `Has Missing Files` field detects whether any files belonging to a completed torrent are missing from disk.

- Only checks **completed torrents**
- Returns `true` if **any** file is missing from its expected path

:::note
Requires "Local filesystem access" enabled on the instance.
:::

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
2. **Cumulative space tracking** - As each torrent is marked for deletion, its size is added to the projected free space (only when the delete mode actually frees disk bytes)
3. **Stop when satisfied** - Once `Free Space + Space To Be Cleared` exceeds your threshold, remaining torrents no longer match
4. **Cross-seed aware** - Cross-seeded torrents sharing the same files are only counted once to avoid overestimating freed space

**Preview Views for Free Space Rules**

When previewing a delete rule with a Free Space condition, a toggle allows switching between two views:

| View | Description |
|------|-------------|
| **Needed to reach target** | Shows only the torrents that would be removed right now to reach your free-space target. This is the default view and reflects actual delete behavior. |
| **All eligible** | Shows all torrents this rule could remove while free space is low. Useful for understanding the full scope of what the rule could potentially delete (may include cross-seeds that don't directly match filters). |

The toggle only appears for delete rules that use the Free Space condition.

**Preview features:**
- **Path column** - Shows the content path for each torrent with copy-to-clipboard support
- **Export CSV** - Download the full preview list (all pages) as a CSV file for external analysis

**Cross-seed expansion in previews:**

Cross-seeds are only expanded and displayed in the preview when using `Remove with files (include cross-seeds)` mode. In this mode, the preview shows all torrents that would be deleted together, with cross-seeds clearly marked. Other delete modes don't expand cross-seeds in the preview since they either preserve cross-seeds or don't consider them specially.

**Delete mode affects space projection:**

| Delete Mode | Space Added to Projection |
|-------------|---------------------------|
| Remove with files | Full torrent size |
| Preserve cross-seeds (no cross-seeds) | Full torrent size |
| Preserve cross-seeds (has cross-seeds) | 0 (files kept) |

**How preserve cross-seeds works:**

- Cross-seed detection checks if any other torrent shares the same Content Path at evaluation time (before any removals).
- If multiple torrents share the same files, removing them all in one rule run will still keep the files on disk. No disk space is freed from that group because each torrent sees the others as cross-seeds.
- Only non-cross-seeded torrents contribute to the free-space projection when using preserve mode.

**Example:** With 400GB free and a rule "Delete if Free Space < 500GB" using `Remove with files`, the system deletes oldest torrents until the cumulative freed space reaches 100GB, then stops. A 50GB torrent and its cross-seed (same files) only count as 50GB freed, not 100GB.

:::note
The UI and API prevent combining `Remove (keep files)` mode with Free Space conditions. Since keep-files doesn't free disk space, such a rule could never satisfy the free space target and would match indefinitely.
:::

:::note
After removing files, qui waits ~5 minutes before running Free Space deletes again to allow qBittorrent to refresh its disk free space reading. The UI prevents selecting 1 minute intervals for Free Space delete rules.
:::

#### Free Space Source

By default, Free Space uses qBittorrent's reported free space (based on its default download location). If you have multiple disks or want to manage a specific mount point, select "Path on server" and enter the path to that disk.

| Source | Description |
|--------|-------------|
| Default (qBittorrent) | Uses qBittorrent's reported free space |
| Path on server | Reads free space from a specific filesystem path |

:::note
Path on server requires "Local Filesystem Access" to be enabled on the instance.
:::

If you want to manage multiple disks, create one workflow per disk and set a different Path on server for each workflow.

:::note
On Windows, Path on server is not supported and Free Space always uses qBittorrent's reported free space. The UI disables the option and switches legacy workflows back to the default when opened.
:::

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

### Delete Old Completed Torrents

Remove torrents completed over 30 days ago when disk space is low:
- Condition: `Completion On Age > 30 days` AND `State is completed` AND `Free Space < 500GB`
- Action: Remove with files

Deletes oldest matching torrents first, stopping once enough space would be freed to exceed 500GB.

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
- Action: Remove with files (preserve cross-seeds)

Removes torrents from the client, oldest first, until enough space is projected to be freed. Cross-seeded torrents keep their files on disk and don't contribute to the projection. If only cross-seeded torrents match, this may remove many torrents without freeing any disk space.

### Clean Up Old Content with Cross-Seeds

Remove completed torrents and all their cross-seeded copies when they're old enough:
- Tracker: `*`
- Condition: `Completion On Age > 30 days` AND `State is completed`
- Action: Remove with files (include cross-seeds)

When a torrent matches, any other torrents pointing to the same downloaded files are deleted together. Useful for complete cleanup when you no longer need any copy of the content.

### Organize by Tracker

Move torrents to tracker-named categories:
- Tracker: `tracker.example.com`
- Action: Category "example" with "Include Cross-Seeds" enabled
