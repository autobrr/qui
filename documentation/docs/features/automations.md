---
sidebar_position: 2
title: Automations
description: Rule-based automation for torrent management.
---

# Automations

Automations are rules that apply actions to torrents that match conditions. Use them to manage speed limits, delete old torrents, and organize torrents with tags and categories.

## How automations work

qui evaluates automations in **sort order**. For exclusive actions such as delete, the first match wins. Each rule matches torrents with a query builder that supports nested conditions.

- **Automatic**: A background service scans torrents every 20 seconds.
- **Per-rule intervals**: Each rule can have its own interval (minimum 60 seconds, default 15 minutes).
- **Per-rule notifications**: If you configure notification targets, each rule can opt in or out of automation notifications.
- **Manual dry-run**: Run "Dry-run now" from the workflow dialog or "Run dry-run now" from the workflow menu.
- **Debouncing**: qui does not process the same torrent again within 2 minutes.

## Query builder

The query builder supports complex nested conditions with AND/OR groups. Drag conditions to reorder them.

### Available condition fields

#### Identity fields

| Field | Description |
| --- | --- |
| Name | Torrent display name (supports cross-category operators) |
| Hash | Info hash |
| Infohash v1 | BitTorrent v1 info hash |
| Infohash v2 | BitTorrent v2 info hash |
| Magnet URI | Magnet link for the torrent |
| Category | qBittorrent category |
| Tags | Set-based tag matching |
| State | Status filter (see State Values below) |
| Created By | Torrent creator metadata |

#### Path fields

| Field | Description |
| --- | --- |
| Save Path | Download location |
| Content Path | Full path to content |
| Download Path | Session download path from qBittorrent |

#### Size fields (bytes)

| Field | Description |
| --- | --- |
| Size | Selected file size |
| Total Size | Total torrent size |
| Completed | Completed bytes |
| Downloaded | Bytes downloaded |
| Downloaded (Session) | Downloaded in current session |
| Uploaded | Bytes uploaded |
| Uploaded (Session) | Uploaded in current session |
| Amount Left | Remaining bytes |
| Free Space | Free space on disk (configurable source, see [Free Space source](#free-space-source)) |

#### Duration fields (seconds)

| Field | Description |
| --- | --- |
| Added Age | Time since added |
| Completed Age | Time since completed |
| Inactive Time | Time since last activity |
| Seen Complete Age | Time since torrent was last complete |
| ETA | Estimated time to completion |
| Reannounce In | Seconds until next announce |
| Seeding Time | Time spent seeding |
| Time Active | Total active time |
| Max Seeding Time | Configured max seeding time |
| Max Inactive Seeding Time | Configured max inactive seeding time |
| Seeding Time Limit | Torrent seeding time limit |
| Inactive Seeding Time Limit | Torrent inactive seeding time limit |

#### System time fields

When qui evaluates a rule, these fields use qui's current system time. Use them for time windows, such as "only run at night" or "apply different actions on weekends".

| Field | Description |
| --- | --- |
| System Hour | Current hour (`0-23`) |
| System Minute | Current minute (`0-59`) |
| System Day of Week | Current weekday (`0=Sun` to `6=Sat`) |
| System Day | Current day of month (`1-31`) |
| System Month | Current month (`1-12`) |
| System Year | Current year |

#### Progress fields

| Field | Description |
| --- | --- |
| Ratio | Upload/download ratio |
| Ratio Limit | Configured ratio limit |
| Max Ratio | qBittorrent max ratio value |
| Uploaded / Size | Uploaded bytes divided by total torrent size. For cross-seeded torrents, use this instead of Ratio. |
| Progress | Download progress (0-100%) |
| Availability | Distributed copies available |
| Popularity | Swarm popularity metric |

#### Speed fields (bytes/s)

| Field | Description |
| --- | --- |
| Download Speed | Current download speed |
| Upload Speed | Current upload speed |
| Download Limit | Configured download speed limit |
| Upload Limit | Configured upload speed limit |

#### Peer/queue fields

| Field | Description |
| --- | --- |
| Active Seeders | Currently connected seeders |
| Active Leechers | Currently connected leechers |
| Total Seeders | Tracker-reported seeders |
| Total Leechers | Tracker-reported leechers |
| Trackers Count | Number of trackers |
| Queue Priority | Torrent queue priority value |

#### Tracker/status fields

| Field | Description |
| --- | --- |
| Tracker | Primary tracker (URL, domain, or customization display name) |
| Trackers (All) | All tracker URLs/domains/display names for this torrent |
| Private | Boolean: the torrent uses a private tracker |
| Unregistered | Boolean: the tracker reports unregistered (requires qBittorrent 5.1+) |
| Tracker status | Per-tracker announce status. See [Tracker status values](#tracker-status-values) (requires qBittorrent 5.1+). |
| Tracker message | Per-tracker status message. Use `nil` for an empty message (requires qBittorrent 5.1+). |
| Comment | Torrent comment field |

If you configured [Tracker Customizations](./tracker-customizations.md) (Dashboard → **Tracker Breakdown**), the **Tracker** condition can also match the display name in addition to the raw URL or domain.

#### Mode fields

| Field | Description |
| --- | --- |
| Auto-managed | Managed by automatic torrent management |
| First/Last Piece Priority | First and last pieces are prioritized |
| Force Start | Ignores queue limits and starts immediately |
| Sequential Download | Downloads pieces sequentially |
| Super Seeding | Super-seeding mode enabled |

#### Release/grouping fields

| Field | Description |
| --- | --- |
| Content Type | Derived from release name parsing (useful for grouping, can be empty) |
| Effective Name | Normalized title derived from release parsing (useful for grouping, can be empty) |
| Release Source | Parsed release specifier (for example `WEBDL`, `WEBRIP`, `BLURAY`, can be empty) |
| Release Resolution | Parsed release specifier (for example `1080p`, can be empty) |
| Release Codec | Parsed release specifier (for example `HEVC`, can be empty) |
| Release HDR | Parsed release specifier (for example `DV`, `HDR`, can be empty) |
| Release Audio | Parsed release specifier (for example `TrueHD`, can be empty) |
| Release Channels | Parsed release specifier (for example `5.1`, can be empty) |
| Release Group | Parsed release specifier (for example `NTb`, can be empty) |
| Release Year | Year parsed from the torrent name (for example `2021`). Numeric, best for movies and dated releases. Releases with no detectable year (including most TV episodes such as `S14E05`) never match any comparison operator, including `!=`. The condition's NOT toggle wraps the whole leaf, so `NOT (year = X)` still matches yearless releases |
| Group Size | Size of the selected group for this condition (requires grouping. See [Grouping](#grouping)). |
| Is Grouped | Boolean: true when selected group size > 1 (requires grouping. See [Grouping](#grouping)). |

#### Cross-seed fields

| Field | Description |
| --- | --- |
| Exists on Other Instance | Boolean: a matching torrent exists on at least one other active instance |
| Seeding on Other Instance | Boolean: a matching torrent is actively seeding on at least one other active instance |
| Cross-seed Exists on Same Instance | Boolean: another matching torrent exists on this instance |
| Cross-seed Seeding on Same Instance | Boolean: another matching torrent is actively seeding on this instance |
| Cross-seed Tags | String: the tags of this torrent and all of its same-instance cross-seeds as one set. NOT operators match only when no copy has the tag. Same matching rules as **Tags** (see [Tag conditions](#tag-conditions)). If there are no cross-seeds, qui checks only the torrent's own tags. |

#### Filesystem fields

| Field | Description |
| --- | --- |
| Hardlink Scope | `none`, `torrents_only`, `inside_qbittorrent`, or `outside_qbittorrent` (requires local filesystem access, see [Hardlink detection](#hardlink-detection)) |
| Hardlink Scope (Cross-Instance) | `none`, `torrents_only`, `inside_qbittorrent`, or `outside_qbittorrent` across all instances (requires local filesystem access) |
| Has Missing Files | Boolean: a completed torrent has files missing on disk (requires local filesystem access) |

### State values

The State field matches these status buckets:

| State | Description |
| --- | --- |
| `downloading` | Actively downloading |
| `uploading` | Actively uploading |
| `completed` | Download finished |
| `stopped` | Paused by user |
| `active` | Has transfer activity |
| `inactive` | No current activity |
| `running` | Not paused |
| `stalled` | No peers available |
| `stalled_uploading` | Stalled while uploading |
| `stalled_downloading` | Stalled while downloading |
| `errored` | Has errors |
| `tracker_down` | Tracker unreachable |
| `tracker_error` | A tracker returned an explicit error |
| `checking` | File check in progress |
| `checkingResumeData` | Checking resume data |
| `moving` | Moving files |
| `missingFiles` | Files not found |
| `unregistered` | Tracker reports unregistered |

`tracker_down` and `tracker_error` require qBittorrent 5.1+ (Web API 2.11.4+). On older instances, the query builder disables these values.

### Tracker status values

If **any** of a torrent's real trackers reports the selected status, the **Tracker status** field matches. qui ignores qBittorrent's DHT/PeX/LSD pseudo-trackers. Pick a value from the dropdown:

| Status | Description |
| --- | --- |
| Not contacted | The client did not contact the tracker yet |
| Working | The client contacted the tracker and it works |
| Updating | A tracker update is in progress |
| Error | Announce failed (generic) |
| Tracker error | The tracker returned an explicit error |
| Unreachable | The client cannot connect to the tracker |

**Tracker message** matches the per-tracker status message with standard string operators. To match an empty or non-empty message, use the literal value `nil` with **is** or **is not**.

Both fields require **qBittorrent 5.1+** (Web API 2.11.4+). On older instances, the query builder disables them.

### Operators

**String:** equals, not equals, contains, not contains, starts with, ends with, matches regex

**Numeric:** `=`, `!=`, `>`, `>=`, `<`, `<=`, between

**Boolean:** is, is not

**State:** is, is not

**Cross-Category (Name field only):**

- `EXISTS_IN`: exact name match in the target category
- `CONTAINS_IN`: partial or normalized name match in the target category

### Regex support

There are two ways to use regex in filter conditions:

**The `matches regex` operator** treats the value as a regex pattern. If the pattern matches anywhere in the field value, the condition is true.

**The regex toggle (`.*` button)** appears next to the value input on the other string operators: `equals`, `contains`, `not contains`, `starts with`, and `ends with`. When you enable the toggle, qui treats the value as a regex pattern.

:::warning Regex toggle changes the selected operator

When the regex toggle is on, `equals`, `contains`, `starts with`, and `ends with` all behave as `matches regex`. Exact, containment, prefix, and suffix matching do not apply.

`not equals` and `not contains` invert the regex result. They are true only when the pattern does **not** match.

You can also negate a regex match with the **NOT toggle** (the `IF / IF NOT` button at the start of the condition row) together with the `matches regex` operator.
:::

qui supports full RE2 (Go regex) syntax. Patterns are case-insensitive by default. A regex is not anchored. Use `^` and `$` for a full-string match (example: `^BHD$`).

Field notes:

- **Tracker**: qui checks the pattern against multiple candidates (raw URL, extracted domain, and the optional customization display name). If a regex is negative, it passes only when **none** of the candidates match.
- **Tags**: If you do not use regex, qui applies string operators per tag. If you turn regex on, qui matches the pattern against the full raw tags string.

The UI validates patterns and shows an error for invalid regex.

### Tag conditions

Each tag condition checks against a **single value**. The value field does not support comma-separated lists. If you enter `tag1, tag2, tag3` as the value, qui treats it as one literal string, not three tags.

If you do not use regex, tag operators (`equals`, `not equals`, `contains`, `not contains`) compare the condition value against each of the torrent's tags.

- `equals` / `not equals`: exact tag membership (case-insensitive)
- `contains` / `not contains`: substring match per tag (case-insensitive)

:::warning Tag operators: `contains` is substring matching
`Tags contains tag1` also matches torrents tagged `tag10`. For exact tag membership, use `equals` / `not equals` with one condition per tag and combine them with an **OR group**.
:::

#### Matching any of multiple tags

To check whether a torrent has **any one of** several tags, create an **OR group** with one condition per tag (exact match):

| # | Field | Operator | Value |
| --- | --- | --- | --- |
| 1 | Tags | equals | tag1 |
| 2 | Tags | equals | tag2 |
| 3 | Tags | equals | tag3 |

Group these conditions with **OR** logic. When at least one tag is present, the rule matches.

To **exclude** torrents that have any of several tags, create an **AND group** of `not equals` conditions (exact match):

| # | Field | Operator | Value |
| --- | --- | --- | --- |
| 1 | Tags | not equals | tag1 |
| 2 | Tags | not equals | tag2 |
| 3 | Tags | not equals | tag3 |

Group these conditions with **AND** logic. All three must be true, so none of those tags is present.

#### Using regex for tag matching

As an alternative to multiple conditions, you can use regex to match several tags in one condition. If you enable regex, the pattern matches against the **full raw tag string** (for example `cross-seed, noHL, racing`), not each tag on its own.

For example, to exclude torrents tagged with `tag1` or `tag2`, use a single condition:

- Field: `Tags`
- Toggle: `IF NOT` (negate the match)
- Operator: `matches regex`
- Value: `(^|,\s*)(tag1|tag2)(\s*,|$)`

qui evaluates the regex against the raw tags string. The delimiter-aware pattern prevents `tag1` from matching `tag10`. The `IF NOT` toggle negates the result. The condition evaluates to true only for torrents that have neither tag.

### Live impact preview and dry-run

When you edit workflows, qui provides immediate feedback for delete and category workflows:

- When conditions and actions change, the **Live impact preview** in the workflow dialog updates.
- It shows the current **impacted count** and a preview list of matching torrents.
- For category rules, the preview summary splits direct matches and cross-seed expansions.

To run a dry-run immediately without waiting for interval execution:

- **Workflow dialog:** `Dry-run now`
- **Workflow list menu:** `Run dry-run now`

A dry-run executes the current workflow configuration as a simulation and writes results to automation activity.

No-match behavior:

- If nothing matches, manual dry-runs log a `dry_run_no_match` summary row.
- To avoid event noise, scheduled dry-run rules do **not** log no-match rows.

## Torrent sorting and scoring

By default, qui processes matched torrents oldest first. Set the **Torrent Priority** to control which torrents qui processes first. This priority setting affects actions such as **Delete** combined with **Free Space**, where priority decides which torrents qui removes first to free space.

### Priority types

| Type | Description |
| --- | --- |
| **Default**| Standard oldest-first priority. |
| **Simple** | Prioritize by one numeric, duration, or string field (for example `Size`, `Added Age`, `Name`) in ascending or descending order. |
| **Score** | Rule-based priority. qui scores torrents with custom rules and sorts them by total score, ascending or descending. |

### Score-based priority

Score-based priority ranks torrents by combined factors. You define **Score Rules**. Each rule evaluates the torrent and adds to its total score.

Available score rule types:
- **Field Multiplier**: Extracts a numeric value from the torrent (such as `Size` or `Time Active`), multiplies it by a configured multiplier, and adds the result to the score.
- **Conditional**: Evaluates a standard query condition (see [Query builder](#query-builder)). If the condition is true, qui adds a static value to the score.

qui processes torrents by their final score. The **Live impact preview** shows the computed scores.

## Tracker matching

You can also scope trackers with a **Tracker** condition, but the tracker pattern field remains available.

| Pattern | Example | Matches |
| --- | --- | --- |
| All | `*` | Every tracker |
| Exact | `tracker.example.com` | Only that domain |
| Glob | `*.example.com` | Subdomains |
| Suffix | `.example.com` | Domain and subdomains |

Separate multiple patterns with commas, semicolons, or pipes. All matching is case-insensitive.

## Grouping

Grouping allows an automation to treat related torrents as one unit for:

- **Group-aware conditions**: `GROUP_SIZE`, `IS_GROUPED`
- **Group expansion**: Apply an action to every torrent in the group, not only the matched torrent.
- **Strict matching**: Grouped expansion runs only when all members satisfy the action conditions.

### Group-scoped condition fields

You can scope `GROUP_SIZE` and `IS_GROUPED` per condition row:

- If you want explicit per-row grouping, set `groupId` on each `GROUP_SIZE` / `IS_GROUPED` condition.
- If a grouped condition row has no `groupId`, qui uses `conditions.grouping.defaultGroupId`.
- If no default is configured, legacy unscoped grouped conditions fall back to `cross_seed_content_save_path`.

Multiple grouped conditions can then live in the same workflow, each with a different grouping strategy.

### Action expansion

Some actions accept a `groupId`. When you set `groupId` on an action, qui expands the action to all torrents in that group.

Group expansion uses strict semantics:

- Every member in the expanded group must satisfy the action condition checks for that rule.
- If any member fails or qui cannot resolve the group, qui skips the entire grouped action.
- If `groupId` is set, there is no "trigger-only fallback".

Built-in group IDs:

- `cross_seed_content_path`: Same Content Path (normalized)
- `cross_seed_content_save_path`: Same Content Path + Save Path (normalized)
- `release_item`: Same Content Type + Effective Name
- `tracker_release_item`: Same Tracker + Content Type + Effective Name
- `hardlink_signature`: Same physical file set signature (requires local filesystem access)

### Custom groups (advanced)

Rules can define custom groups via `conditions.grouping.groups[]` with:

- `id`: string
- `keys`: list of key names combined to form the group key

Supported keys:

- `contentPath`, `savePath`, `effectiveName`, `contentType`, `tracker`
- `rlsSource`, `rlsResolution`, `rlsCodec`, `rlsHDR`, `rlsAudio`, `rlsChannels`, `rlsGroup`
- `hardlinkSignature`

For content-path-based grouping where `Content Path == Save Path` (ambiguous), you can set:

- `ambiguousPolicy: "verify_overlap"` (default for cross-seed groups) with `minFileOverlapPercent` (default `90`)
- `ambiguousPolicy: "skip"`

## Actions

You can combine actions, except Delete. Delete must be standalone. Each action supports an optional condition override.

### Speed limits

Set upload or download limits. Each field supports these modes:

| Mode | Value | Description |
| --- | --- | --- |
| No change | - | Do not change this field |
| Unlimited | 0 | Remove speed limit (qBittorrent treats 0 as unlimited) |
| Custom | >0 | Specific limit in KiB/s or MiB/s |

qui applies limits in batches.

### Share limits

Set ratio limits or seeding time limits. Each field supports these modes:

| Mode | Value | Description |
| --- | --- | --- |
| No change | - | Do not change this field |
| Use global | -2 | Follow qBittorrent's global share settings |
| Unlimited | -1 | No limit for this field |
| Custom | >=0 | Specific value (ratio as decimal, time in minutes) |

When a torrent reaches any enabled limit, it stops seeding.

#### Share limit action (Web API 2.15.1+)

If the qBittorrent Web API is **2.15.1** or newer, the torrent share limit dialog and automation workflows show **When limits are reached**. When a torrent reaches its ratio, seeding time, or inactive seeding limit, this setting controls what happens. qui stores and sends the **string enum names** that qBittorrent expects for `setShareLimits` (Qt meta-object names, not numeric codes):

| Option | Value (`shareLimitAction`) | Description |
| --- | --- | --- |
| Default (use global) | omit or `default` | Follow qBittorrent's global setting |
| Stop torrent | `Stop` | Pause the torrent |
| Remove torrent | `Remove` | Remove from client, keep files |
| Remove with content | `RemoveWithContent` | Remove from client and delete files |
| Enable super seeding | `EnableSuperSeeding` | Switch to super seeding mode |

#### Share limits matching mode (Web API 2.16.0+)

**Limits matching mode** (match **any** limit or **all** limits) is a separate Web API capability and requires **2.16.0** or newer. If you run older 5.2 builds that expose only **2.15.1**, qui shows the action above but hides this control until you upgrade qBittorrent. Values use **string enum names** for `setShareLimits`:

| Option | Value (`shareLimitsMode`) | Description |
| --- | --- | --- |
| Default (use global) | omit or `default` | Follow qBittorrent's global setting |
| Match any limit | `MatchAny` | Trigger when any single limit is reached |
| Match all limits | `MatchAll` | Trigger only when all limits are reached |

If the instance does not report the required Web API version, qui hides these options (see [qBittorrent Version Compatibility](../advanced/compatibility.md)). Ratio and seeding time limits above still apply on older instances. qui gates only the extra controls.

If the connected qBittorrent instance supports these fields, they appear in the torrent share limit dialog and the automation workflow editor. On older instances, qui hides the fields and sends only the classic ratio and seeding time limits.

### Pause

Pause matching torrents. qui pauses only torrents that are not already stopped.

If a resume action is also present, the last action wins.

### Resume

Resume matching torrents. qui resumes only torrents that are not already running.

If a pause action is also present, the last action wins.

### Force recheck

Force recheck matching torrents.

- Triggers a qBittorrent recheck for matched torrents.
- You can combine this action with other actions.
- Supports an optional condition override, as other actions do.

### Force reannounce

Force reannounce matching torrents.

- Triggers an immediate tracker reannounce for matched torrents.
- You can combine this action with other actions.
- Supports an optional condition override, as other actions do.

### Delete

Remove torrents from qBittorrent. **Delete must be standalone.** You cannot combine it with other actions.

| Mode | Description |
| --- | --- |
| `delete` | Remove from client, keep files |
| `deleteWithFiles` | Remove with files |
| `deleteWithFilesPreserveCrossSeeds` | Remove files, but keep them if qui detects cross-seeds |
| `deleteWithFilesIncludeCrossSeeds` | Remove files and also delete all cross-seeded torrents sharing the same files |

**Optional grouping (advanced):**

Delete actions can specify a `groupId` to expand the deletion to all torrents in that group.

- If you want "remove from client" to be cross-seed aware, use `groupId` for `delete` (keep files).
- If you set `groupId`, a keep-files delete is strict all-or-none. If any group member does not satisfy the rule conditions, qui removes nothing in that group.

**Include cross-seeds mode behavior:**

When a torrent matches the rule, qui finds other torrents that point to the same downloaded files (cross-seeds and duplicates) and deletes them together. Use this mode to remove content and all its cross-seeded copies at once.

- **Safe expansion**: If qui cannot determine that another torrent uses the same files, qui does not include it in the deletion.
- **File lists decide, not paths**: qui includes a candidate only when at least 90% of the smaller torrent's bytes use the same file locations. A torrent that shares only the folder, such as a pack with a top folder named `Season 2`, keeps its files and stays in the client. If overlap is above zero but below 90%, qui skips the entire deletion group.
- **Safety-first**: If the file check cannot complete for any reason, qui skips the entire group to prevent broken torrents.
- **Preview**: The delete preview shows all torrents that qui deletes in simulation, with cross-seeds marked.

**Include hardlinked copies:**

If you enable "Include hardlinked copies" (available only with `deleteWithFilesIncludeCrossSeeds` mode), qui also deletes torrents that share the same physical files through hardlinks, even when their Content Paths differ.

- **Requires**: You must enable Local Filesystem Access on the instance.
- **Safe scope**: qui includes only hardlinks fully contained within qBittorrent's torrent set. qui never follows hardlinks to files outside qBittorrent (for example, your media library).
- **Preview**: The preview marks hardlink-expanded torrents as "Cross-seed (hardlinked)".
- **Free Space projection**: If you use Free Space conditions, qui deduplicates hardlink groups in the space projection. Torrents that share the same physical files count once.

If qBittorrent holds hardlinked copies of content in different locations and you want to remove all copies together, use this mode.

### Tag

Manage tags on torrents. You can add multiple Tag actions in one workflow.

| Mode | Description |
| --- | --- |
| `full` | Add to matches, remove from non-matches (smart toggle) |
| `add` | Only add to matches |
| `remove` | Only remove from matches |

:::note
`mode: remove` removes tags from torrents that match the tag action condition. It does not remove from non-matches.
:::

qui evaluates `mode: full` within the rule's scope for that run (enabled rule, tracker pattern match, and run eligibility). It is not a client-wide sweep by itself.

Options:

- **Managed / Replace in Client**: `Managed` (default) applies per-torrent add/remove diffs only. `Replace in client` deletes managed tags from qBittorrent first, then reapplies them to current matches.
- **Use tracker name as tag**: Derive the tag from the tracker domain.
- **Use display name**: Use the [tracker customization](./tracker-customizations.md) display name instead of the raw domain.

Behavior reference:

| Configuration | Behavior |
| --- | --- |
| `mode: full` + `Managed` | Adds/removes tag for torrents this rule evaluates. No client-wide reset. |
| `mode: full` + `Replace in client` | Deletes selected tag(s) client-wide first, then re-adds only current matches. |

If you see repeated activity such as `+tag=696` on every run, you likely enabled **Replace in client** for that tag action.

Quick troubleshooting:

1. Look in the logs for `automations: deleted managed tags from client before retagging`.
2. In the Automations UI, open enabled rules and look for "Replace in client" on any tag action.
3. Make sure that the activity entry's rule list matches the expected rule.

### Category

Move torrents to a different category.

Options:

- **Include affected cross-seeds**: Also move cross-seeds (torrents with matching ContentPath AND SavePath).
- **Group ID (advanced)**: Expand category changes to all torrents in the specified group (see [Grouping](#grouping)). If set, this option takes precedence over "Include affected cross-seeds".
- **Strict grouped matching**: If you set `groupId`, category expansion applies only when all group members satisfy the category rule checks.
- **Skip if cross-seed exists in categories**: If another cross-seed is in a protected category, prevent the move.

### Move

Move torrents to a different path on disk. If AutoTMM is off, use this action to move content.

Options:
- **Group ID (advanced)**: Expand moves to all torrents in the specified group (see [Grouping](#grouping)). qui resolves the move path for the matched torrent and applies it to the whole group.
- **Strict grouped matching**: If you set `groupId`, move expansion is all-or-none. Every member must satisfy the move rule checks.
- **Skip if cross-seeds don't match the rule's conditions**: If the torrent has cross-seeds that do not match the rule's conditions, skip the move. If **Group ID** is set, qui ignores this option.

#### Move path templates

qui evaluates the move path as a **Go template** for each torrent. Use a fixed path (for example `/data/archive`) or template actions to build paths from torrent properties.

**Available template variables:**

| Variable | Description |
| --- | --- |
| `.Name` | Torrent display name |
| `.Hash` | Info hash |
| `.Category` | qBittorrent category |
| `.IsolationFolderName` | Filesystem-safe folder name (hash or sanitized name) |
| `.Tracker` | Tracker display name from [Tracker Customizations](./tracker-customizations.md), otherwise the tracker domain |

**Template function:**

| Function | Description |
| --- | --- |
| `sanitize` | Makes a string safe for use as a path segment (removes invalid characters). Use it for user-controlled values such as names, for example `{{ sanitize .Name }}`. |

**Examples:**

- Fixed path (no template actions): `/data/archive`
- By category: `/data/{{.Category}}` → for example `/data/movies`
- By name (safe for paths): `/data/{{ sanitize .Name }}`
- By isolation folder: `/data/{{.IsolationFolderName}}`
- By tracker: `/data/{{.Tracker}}` (when a tracker display name is configured)

:::note
If you want `.Tracker` to use your [tracker customization](./tracker-customizations.md) display name, the rule also needs a **Tracker** condition. A tag action with **Use tracker name as tag** and **Use display name** enabled also works. Without one of those settings, `.Tracker` falls back to the tracker domain, and qui names your folders after the domain instead.
:::

### Auto management

Enable or disable qBittorrent's Automatic Torrent Management (AutoTMM) on matching torrents.

| Mode | Description |
| --- | --- |
| `enable` | Enable automatic torrent management on matches |
| `disable` | Disable automatic torrent management on matches |

When AutoTMM is on, qBittorrent moves torrents to the save path configured for their category. When AutoTMM is off, you control save paths manually.

If multiple rules match the same torrent with Auto Management actions, the **last matching rule** (by sort order) wins.

### External Program

When torrents match the automation rule, run a configured external program. This action uses the programs from **Settings → External Programs**.

| Field | Description |
| --- | --- |
| **Program** | Select from enabled external programs |
| **Condition Override** | Optional condition specific to this action |

**Behavior:**

- The program runs asynchronously (fire-and-forget) and does not block automation processing.
- You can combine this action with other actions (speed limits, share limits, pause, tag, category).
- Only enabled programs appear in the dropdown.
- qui logs activity with the rule name, torrent details, and success or failure status.

:::note
To appear in the automation dropdown, the program must be enabled in **Settings → External Programs**.
:::

:::note
When multiple rules match the same torrent with External Program actions enabled, the **last matching rule** (by sort order) determines which program executes for that torrent. Only one program runs per torrent per automation cycle.
:::

:::warning
The executable path of the program must be in the allowlist. Disabled programs and programs with forbidden paths do not run. qui rejects the execution attempt and logs the rejection in the activity log with the rule name and torrent details.
:::

**Use cases:**

- Run post-processing scripts when torrents complete.
- When conditions match, notify external systems (webhooks, notifications).
- Trigger media library scans after category changes.
- Run cleanup scripts for old or stalled torrents.

### Export to Instance

Export a torrent's `.torrent` file from the current instance and add it to a different qBittorrent instance. Use this action to migrate torrents between instances, for example from a seedbox to a local instance for long-term seeding.

The action assumes the data already exists on the target (moved with rclone, Quickdrop for Deluge, or another tool) and uses `skip_checking=true` by default.

| Field | Description |
| --- | --- |
| **Target instance** | Destination qBittorrent instance (cannot be the same as source) |
| **Save path** | Save path on target instance (Go template supported, see below) |
| **Category** | Category to assign on target instance (dropdown from target's categories) |
| **Tags** | Tags to apply on target instance |
| **Skip checking** | Skip hash check on target (default: enabled) |
| **Paused** | Add torrent paused on target |
| **Content layout** | `Default` (target instance setting), `Original`, `Create subfolder`, or `Don't create subfolder` |
| **Condition Override** | Optional condition specific to this action |

**Behavior:**

- The export runs asynchronously and does not block automation processing.
- **You cannot combine it with Delete.** The API rejects rules that enable both export and delete.
- Duplicate detection: Before the export, qui looks for the torrent on the target instance and skips the export if found.
- After the add, qui verifies that the torrent appeared and is healthy. If that check fails, qui removes the torrent from the target so the next run can retry the transfer.
- qui does **not** export cross-seed group members. To export a group, chain the action with Category/Tag actions that use group expansion.
- qui logs activity with the rule name, torrent details, target instance, and success or failure status.
- Dry-run shows what qui exports in simulation, without a transfer.

:::note
When multiple rules match the same torrent with Export to Instance actions, the **last matching rule** (by sort order) determines the export configuration for that torrent. Only one export runs per torrent per automation cycle.
:::

#### Save path templates

The save path field supports Go templates, the same as the [Move action](#move-path-templates).

| Variable | Description |
| --- | --- |
| `.Name` | Torrent display name |
| `.Hash` | Info hash |
| `.Category` | qBittorrent category (on source instance) |
| `.IsolationFolderName` | Filesystem-safe folder name (hash or sanitized name) |
| `.Tracker` | Tracker display name from [Tracker Customizations](./tracker-customizations.md), otherwise the tracker domain |

| Function | Description |
| --- | --- |
| `sanitize` | Makes a string safe for use as a path segment (removes invalid characters). |

**Examples:**

- Fixed path: `/data/torrents`
- By category: `/data/{{.Category}}`
- By tracker: `/data/{{.Tracker}}`

If you set no save path but set a category, qui enables qBittorrent's Automatic Torrent Management so the target uses the category's configured path.

## Cross-seed awareness

Automations detect cross-seeded torrents (same content and files) and can handle them specially:

- **Detection**: Cross-seed condition fields match on content path, exact name, and release metadata. Same-instance checks exclude the current torrent itself. **Filter Cross-Seeds** also pairs a retitled upload that has the same exact byte size. The condition fields do not pair retitled uploads, because a rule can delete torrents and a title is weaker evidence than a shared file.
- **Delete rules**:
  - If cross-seeds exist, use `deleteWithFilesPreserveCrossSeeds` to keep files.
  - Use `deleteWithFilesIncludeCrossSeeds` to delete matching torrents and all their cross-seeds together.
- **Category rules**: Enable "Include affected cross-seeds" to move related torrents together.
- **Blocking**: If cross-seeds are in protected categories, prevent category moves.

### Hardlink detection

The `HARDLINK_SCOPE` field lets automations tell apart torrents whose files are hardlinked into an external library (Sonarr, Radarr, and similar tools) and torrents that exist only within qBittorrent. This field serves as the foundation for safe "Remove Upgraded Torrents" automations.

#### How scope is determined

When an automation references `HARDLINK_SCOPE`, qui validates and inspects every file of every torrent in qBittorrent. qui ignores a priority-0 (`Do not download`) file that does not exist on disk. qui scans an existing priority-0 file like any other regular file. For each regular file, qui extracts:

- The **inode** and **device ID**: These identify the file on disk.
- The **nlink count**: The total number of hardlinks to that inode, as the filesystem reports it.

qui counts how many unique file paths across the qBittorrent torrent set point to each inode. qui compares these two numbers to set the scope for each torrent:

| Scope | Condition | Meaning |
| --- | --- | --- |
| `none` | No file has `nlink > 1` | No hardlinks detected. |
| `torrents_only` | Some file is linked inside the torrent set, no file has outside links | Hardlinks exist, but only between torrents in qBittorrent. No external library links. |
| `outside_qbittorrent` | Some file has `nlink > uniquePathCount`, no file is linked inside the set | Something outside qBittorrent hardlinked the file, typically a Sonarr/Radarr library import. |
| `both` | Some file is linked inside the torrent set, some file also has outside links | Linked to other torrents *and* to an external location (for example, a cross-seeded library import). |

In conditions, two additional predicate values are available on top of the exact scopes above:

- `inside_qbittorrent` matches torrents linked to other torrents in the set, even when they are also linked outside (`torrents_only` or `both`).
- `outside_qbittorrent` as a condition value keeps its historical meaning. It matches any torrent with outside links (`outside_qbittorrent` or `both`).

With AND/OR groups and the "is not" operator, you can express every combination of the inside and outside link states.

:::note
`HARDLINK_SCOPE` reflects only hardlink metadata. qui detects cross-seeds separately with ContentPath matching. A torrent can have `HARDLINK_SCOPE = none` and still be cross-seeded.
:::

#### Unknown scope and safety behavior

If path validation or file inspection fails for **any remaining** file, the torrent receives no scope entry. Causes include invalid paths, missing permissions, and inaccessible storage. All `HARDLINK_SCOPE` conditions evaluate to `false` for that torrent, regardless of the operator or value. This safety measure prevents unintended deletion of torrents that qui cannot fully inspect.

To diagnose this issue, enable debug logging and check for the "hardlink index built" log message, which reports an `inaccessible` count.

#### Docker volume requirements

For hardlink scope detection to work in Docker:

1. **Paths must match exactly.** qui must read files at the exact paths qBittorrent reports. If qBittorrent reports a save path of `/data/torrents/radarr/`, qui must have access to `/data/torrents/radarr/` inside its container.

2. **Same underlying storage.** Both containers must share the same host mount so that inode numbers stay consistent. If qui and qBittorrent access the same files through different host mounts or different bind-mount configurations, inode numbers can differ.

3. **Single mount, not subdivided.** Mount the common parent directory instead of individual subdirectories. For example, if your data lives under `/mnt/media/data` on the host:

```yaml
services:
  qui:
    volumes:
      - /home/user/docker/qui:/config
      - /mnt/media/data:/data # single mount covering both torrents and library
```

Do not mount both `/mnt/media/data/torrents:/data/torrents` **and** `/mnt/media/data:/data`. Overlapping mounts cause inconsistent inode visibility. Use one mount at the common parent.

#### Filesystem limitations

Hardlink scope detection depends on the kernel reporting accurate `nlink` values in stat results. Some filesystems do not report accurate values:

- **FUSE-based filesystems** (sshfs, mergerfs, rclone mount) can report `nlink = 1` for all files, regardless of the actual hardlink count.
- **Some NAS appliance filesystems** and **overlay filesystems** (overlayfs) behave the same way.
- **Network filesystems** (NFS, CIFS/SMB) usually report accurate nlink values, but behavior varies by server.

On affected filesystems, every torrent appears to have scope `none` because nlink is always 1. qui has no workaround for this kernel and filesystem limitation. If you suspect this issue, run `stat` on a file with hardlinks and read the "Links" count.

Hardlinks cannot span different filesystems. If your torrent data and media library live on separate filesystems, or on Docker volumes with different host paths, Sonarr and Radarr copy files instead. Scope detection then finds nothing.

#### Example: Remove Upgraded Torrents

This automation deletes torrents that Sonarr/Radarr replaced with an upgrade. It targets torrents whose library hardlink no longer exists (the arr removed or re-linked it during the upgrade). The rule also requires at least 7 days of seeding time and a category that matches your arr categories.

:::tip
Use `HARDLINK_SCOPE` with `NOT_EQUAL` to `outside_qbittorrent` rather than `EQUAL` to `none`. Torrents with scope `torrents_only` (cross-seeded but not in a library) then stay eligible for cleanup, and any torrent still linked into your media library stays protected.
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
              {
                "field": "CATEGORY",
                "operator": "EQUAL",
                "value": "radarr.cross"
              },
              {
                "field": "CATEGORY",
                "operator": "EQUAL",
                "value": "tv-sonarr"
              },
              {
                "field": "CATEGORY",
                "operator": "EQUAL",
                "value": "tv-sonarr.cross"
              }
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

When Sonarr or Radarr upgrades a release, it removes the old library hardlink. The old torrent's files then have `nlink == 1` (scope `none`) or link only to other torrents (scope `torrents_only`). In both cases, the scope is not `outside_qbittorrent`. The automation matches and deletes the torrent once the torrent meets the seeding time requirement.

If the automation matches torrents that you expect to stay protected, make sure that:

1. qui can access all torrent files at the paths qBittorrent reports (the debug log lists inaccessible files).
2. Your filesystem reports accurate nlink values (`stat <file>` shows Links > 1 for hardlinked files).
3. Your Docker volume mounts do not overlap or subdivide storage in a way that breaks inode consistency.

### Cross-instance hardlink detection

The `HARDLINK_SCOPE_CROSS` field extends hardlink detection across **all** configured qBittorrent instances. While `HARDLINK_SCOPE` considers only torrents within one instance, `HARDLINK_SCOPE_CROSS` accounts for hardlinks to files managed by any instance with local filesystem access enabled.

Use this field in multi-instance setups where cross-seeds are hardlinked across instances. Without cross-instance awareness, those hardlinks appear as `outside_qbittorrent` even though they point to files managed by another qBittorrent instance.

#### Scope values

| Scope | Meaning |
| --- | --- |
| `none` | No hardlinks detected. |
| `torrents_only` | All hardlinks are accounted for across all qBittorrent instances. |
| `outside_qbittorrent` | Hardlinks exist to files outside all qBittorrent instances. |
| `both` | Hardlinks exist to other torrents across instances and to files outside all instances. |

The condition values `inside_qbittorrent` and `outside_qbittorrent` also match `both`, as they do for `HARDLINK_SCOPE`.

#### Combining with HARDLINK_SCOPE

If you use both fields together, you can distinguish cross-instance hardlinks from truly external links:

| Combination | Interpretation |
| --- | --- |
| `HARDLINK_SCOPE = outside_qbittorrent` AND `HARDLINK_SCOPE_CROSS = torrents_only` | Hardlinks point to other qBittorrent instances only (cross-seeds). No media library copy. |
| `HARDLINK_SCOPE = outside_qbittorrent` AND `HARDLINK_SCOPE_CROSS = outside_qbittorrent` | Hardlinks point outside all instances, typically a media library import. |
| `HARDLINK_SCOPE = torrents_only` | All hardlinks within this instance. `HARDLINK_SCOPE_CROSS` will also be `torrents_only`. |

#### Prerequisites

`HARDLINK_SCOPE_CROSS` requires:

1. **Local Filesystem Access** enabled on **all** instances whose files you want considered. qui skips instances without this access. Their files are not scanned, and unresolved hardlinks show as `outside_qbittorrent`.
2. **Same filesystem** across all instances. Hardlinks cannot cross filesystem boundaries.
3. **Matching paths in Docker**: The same volume mount requirements as `HARDLINK_SCOPE`, applied to every instance.

#### Performance

The cross-instance scan runs only when:
- A rule uses `HARDLINK_SCOPE_CROSS`
- The single-instance scan finds torrents with unresolved outside links

The scan uses cached torrent and file data from other instances without extra API calls. It calls `Lstat()` only on files that can resolve unaccounted hardlinks. The scan stops as soon as qui resolves all deficits.

A safety budget of 500,000 `Lstat()` calls limits the cross-instance scan. If the scan exhausts the budget before resolving all deficits, the remaining torrents report `outside_qbittorrent`. This limit restricts filesystem operations in large multi-instance setups. When the scan reaches the budget, qui logs a warning.

#### Example: noHL tagging in multi-instance setups

If torrents have no media library hardlinks, this rule tags them with `noHL`, even when they have cross-instance hardlinks to other qBittorrent instances:

```json
{
  "name": "Tag noHL (multi-instance)",
  "trackerPattern": "*",
  "trackerDomains": ["*"],
  "conditions": {
    "schemaVersion": "1",
    "tags": [
      {
        "enabled": true,
        "mode": "add",
        "tags": ["noHL"],
        "condition": {
          "operator": "AND",
          "conditions": [
            {
              "field": "HARDLINK_SCOPE_CROSS",
              "operator": "NOT_EQUAL",
              "value": "outside_qbittorrent"
            },
            {
              "field": "STATE",
              "operator": "EQUAL",
              "value": "uploading"
            }
          ]
        }
      }
    ]
  }
}
```

This configuration works because `HARDLINK_SCOPE_CROSS != outside_qbittorrent` matches both `none` (no hardlinks) and `torrents_only` (hardlinks only between qBittorrent instances). Torrents with a media library copy (`outside_qbittorrent`) do not receive the tag.

## Missing files detection

The `Has Missing Files` field detects whether any file of a completed torrent is missing from disk.

- qui checks **completed torrents** only.
- If **any** file is missing from its expected path, this condition returns `true`.

:::note
Requires "Local filesystem access" enabled on the instance.
:::

## Important behavior

### Settings only set values

Automations apply settings but **do not revert** them when you disable or delete a rule. If a rule sets the upload limit to 1000 KiB/s, affected torrents keep that limit until you change it or another rule applies a different value.

### Efficient updates

qui sends an API call only when the torrent's current setting differs from the target value. qui skips no-op updates.

### Processing order

- **First match wins** for delete actions. A delete ends processing for that torrent, and qui evaluates no further rules.
- **Last rule wins** for speed limits, share limits, category, external program, and export to instance actions.
- **Accumulative** for tag actions. qui combines tags across matching rules.

### Free Space condition behavior

When a delete rule uses the **Free Space** condition, qui tracks freed space cumulatively:

1. **Configurable processing order**: qui processes torrents according to the automation's Torrent Priority (Default, Simple, or Score). This lets you prioritize cleanups (for example, largest files first, or lowest score first).
2. **Cumulative space tracking**: As qui marks each torrent for deletion, qui adds its size to the projected free space (only when the delete mode frees disk bytes).
3. **Stop when satisfied**: Once `Free Space + Space To Be Cleared` exceeds your threshold, remaining torrents no longer match.
4. **Cross-seed aware**: qui counts cross-seeded torrents that share the same files once to avoid overestimating freed space.

**Preview views for Free Space rules**

When you preview a delete rule with a Free Space condition, a toggle switches between two views:

| View | Description |
| --- | --- |
| **Needed to reach target** | Only the torrents qui removes now to reach your free-space target. This is the default view and reflects actual delete behavior. |
| **All eligible** | All torrents this rule can remove while free space stays low. It shows the full scope of what the rule can delete (this can include cross-seeds that do not directly match filters). |

The toggle appears only for delete rules that use the Free Space condition.

**Preview features:**

- **Path column**: Shows the content path for each torrent, with copy-to-clipboard support.
- **Export CSV**: Downloads the full preview list (all pages) as a CSV file for external analysis.

**Cross-seed expansion in previews:**

The preview expands and shows cross-seeds only in `Remove with files (include cross-seeds)` mode. In this mode, the preview shows all torrents that qui deletes together, with cross-seeds marked. Other delete modes do not expand cross-seeds in the preview because they either preserve cross-seeds or do not treat them specially.

**Delete mode affects space projection:**

| Delete Mode | Space Added to Projection |
| --- | --- |
| Remove with files | Full torrent size |
| Preserve cross-seeds (no shared files) | Full torrent size |
| Preserve cross-seeds (shared files) | 0 (files kept) |

**How preserve cross-seeds works:**

- Torrents become candidates when they share the same Content Path. qui then compares their resolved file paths and sizes.
- qui does not treat torrents in the same directory that use different files as cross-seeds. qui removes their files.
- If torrents share files or the file comparison cannot complete, qui keeps the files.
- Only torrents whose files qui removes contribute to the free-space projection.

**Example:** If you have 400GB free and a rule "Delete if Free Space < 500GB" in `Remove with files` mode, qui deletes the oldest torrents. It stops when the cumulative freed space reaches 100GB. A 50GB torrent and its cross-seed (same files) count as 50GB freed, not 100GB.

:::note
The UI and API prevent the combination of `Remove (keep files)` mode with Free Space conditions. Keep-files does not free disk space, so such a rule can never reach the free space target and matches forever.
:::

:::note
After qui removes files, it waits about 5 minutes before running Free Space deletes again, so qBittorrent can refresh its disk free space reading. The UI prevents 1 minute intervals for Free Space delete rules.
:::

#### Free Space source

By default, Free Space uses qBittorrent's reported free space, based on its default download location. If you want to manage a specific mount point, select "Path on server" and enter the path to that disk.

| Source | Description |
| --- | --- |
| Default (qBittorrent) | Uses qBittorrent's reported free space |
| Path on server | Reads free space from a specific filesystem path |

:::note
Path on server requires "Local Filesystem Access" enabled on the instance.
:::

If you want to manage multiple disks, create one workflow per disk and set a different Path on server for each workflow.

:::note
qui does not support Path on server on Windows, and Free Space always uses qBittorrent's reported free space there. The UI disables the option and switches legacy workflows back to the default when you open them.
:::

### Batching

qui groups torrents by action value and sends them to qBittorrent in batches of up to 50 hashes per API call.

## Activity log

qui logs every automation action with:

- Torrent name and hash
- Rule name and action type
- Outcome (success/failed) with reasons
- Action-specific details

qui keeps activity for 7 days. View the log in the Automations section for each instance.

## Example rules

### Delete old completed torrents

If disk space is low, remove torrents completed over 30 days ago:

- Condition: `Completed Age > 30 days` AND `State is completed` AND `Free Space < 500GB`
- Action: Remove with files

qui deletes matching torrents in the configured priority order (for example, oldest first) and stops once the projected free space exceeds 500GB.

### Speed limit private trackers

Limit upload on private trackers:

- Tracker: `*`
- Condition: `Private is true`
- Action: Upload limit 10000 KiB/s

### Tag stalled torrents

Auto-tag torrents with no activity:

- Tracker: `*`
- Condition: `Inactive Time > 7 days`
- Action: Tag "stalled" (mode: add)

### Clean unregistered torrents

Remove torrents the tracker no longer recognizes:

- Tracker: `*`
- Condition: `Unregistered is true`
- Action: Delete (keep files)

### Maintain minimum free space

Keep at least 200GB free by removing oldest completed torrents:

- Tracker: `*`
- Condition: `Free Space < 200GB` AND `State is completed`
- Action: Remove with files (preserve cross-seeds)

qui removes torrents from the client in the configured priority order until the projection frees enough space. Cross-seeded torrents keep their files on disk and do not contribute to the projection. If only cross-seeded torrents match, this rule can remove many torrents without freeing any disk space.

### Clean up old content with cross-seeds

Remove completed torrents and all their cross-seeded copies when they are old enough:

- Tracker: `*`
- Condition: `Completed Age > 30 days` AND `State is completed`
- Action: Remove with files (include cross-seeds)

When a torrent matches, qui also deletes every other torrent that points to the same downloaded files. Use this rule when you no longer need any copy of the content.

### Find duplicate releases of the same title

List releases that share a parsed title but come from different release groups, such as `Show.S01.1080p.WEB-DL-GROUP1` and `Show.S01.1080p.WEB-DL-GROUP2`:

- Tracker: `*`
- Condition: `Is Grouped is true` with `groupId` set to `release_item` (see [Grouping](#grouping))
- Action: Tag "dupe" (mode: add)

The **Live impact preview** lists the matches while you edit, so the rule can stay disabled if you only want to look.

:::note
Cross-seeded copies of a release are also members of these groups, because the `release_item` key ignores the release group and the tracker.
:::

### Organize by tracker

Move torrents to tracker-named categories:

- Tracker: `tracker.example.com`
- Action: Category "example" with "Include affected cross-seeds" enabled

### Post-processing on completion

When torrents finish downloading, run a script:

- Tracker: `*`
- Condition: `State is completed` AND `Progress = 100`
- Action: External Program "post-process.sh"

### Notify on stalled torrents

When torrents stall, alert an external monitoring system:

- Tracker: `*`
- Condition: `State is stalled` AND `Inactive Time > 24 hours`
- Action: External Program "send-alert" + Tag "stalled" (mode: add)
