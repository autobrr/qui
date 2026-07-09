---
sidebar_position: 7
title: Season Packs
description: Assemble season packs from individual episodes using autobrr webhooks.
---

# Season Packs

qui can assemble season-pack torrents from individual episodes you already seed. When autobrr announces a season pack, qui checks your qBittorrent instances for completed, release-compatible episodes and, if enough local data is present, builds a linked directory tree, adds the torrent, and lets qBittorrent download anything still missing.

## How It Works

1. autobrr sees a season pack release
2. autobrr sends the torrent name (and optionally the torrent file) to qui's `/api/cross-seed/season-pack/check` endpoint
3. If a torrent file is provided, qui parses its file list to determine playable episode files. If not, qui uses metadata providers for episode counts.
4. qui scans your qBittorrent instances for completed individual episodes that match the season pack's release details
5. qui computes coverage from completed, matching local episodes:
   - When torrent data is provided, the pack torrent's playable episode files define the expected pack layout
   - qui asks Sonarr for the season total first, when Sonarr can resolve the show
   - If Sonarr cannot resolve it, qui falls back to metadata providers: TVDB when configured, then TVMaze
   - With torrent data, qui never uses a total lower than the playable episode count inside the pack torrent
6. qui responds with:
   - `200 OK` - coverage meets the threshold, ready to apply
   - `404 Not Found` - local coverage is too low, the release is not a season pack, or the feature is disabled
7. On `200 OK`, autobrr sends the torrent file to `/api/cross-seed/season-pack/apply`
8. qui links the matched episodes, applies your configured season-pack tags, and adds the season pack torrent
9. If episodes or extras are still missing, qui adds the torrent paused, attempts an automatic recheck, and queues automatic resume. After recheck, qui resumes the torrent when qBittorrent reports progress at or above your configured season-pack coverage threshold. If recheck finishes below that threshold, qui leaves the torrent paused for manual review. Best-effort fallbacks are reported by name, including `automatic recheck failed`, `automatic resume is unavailable`, and `automatic resume queue is full`.

## Coverage Model

qui uses a provider-first episode total with the pack torrent as the layout source.

For `/check` without torrent data:

- qui asks Sonarr for the season episode total first
- If Sonarr fails or cannot resolve the show, qui asks TVDB when configured, otherwise TVMaze
- If no provider returns a total, qui skips threshold enforcement and only verifies that matching episodes exist

For `/check` or `/apply` with torrent data:

- The torrent file is the source of truth for the pack layout and playable episode files
- qui still asks Sonarr, then TVDB/TVMaze, for a season total
- If the provider total is lower than the playable episode count in the torrent, qui uses the playable file count instead

The apply endpoint always requires the torrent file and enforces the threshold.

When qui falls back to the pack torrent, it:

- Counts only playable video files (mkv, mp4, avi, etc.)
- Ignores subtitles, NFOs, samples, and other extras
- Deduplicates episodes that appear more than once
- Rejects packs with zero usable episode files

Coverage is then: `matchedLocalEpisodes / coverageTotalEpisodes`

For an episode to count toward coverage, it must:

- Be fully downloaded (`100%` progress)
- Pass the same release-compatibility checks used by normal cross-seeding
- Belong to the same episode in the season pack

This means mixed variants do **not** count toward coverage. For example, `720p WEB` episodes do not satisfy a `1080p BluRay` season pack.

The default threshold is **75%**. Change it in **Cross-Seed > Rules > Season packs** in the qui UI.

## Matching Settings

These settings only affect season-pack checks and applies. They do not change normal cross-seed matching in the Rules tab.

Defaults are chosen to match common seasonpackarr expectations.

| Setting | Default | Effect | Example |
| --- | --- | --- | --- |
| Ignore REPACK/PROPER differences | On | Treat REPACK and PROPER episodes as compatible with the season pack. | `Show.S01E01.REPACK` matches `Show.S01E01.PROPER` |
| Simplify HDR matching | Off | Treat HDR10, HDR10+, and HDR+ as HDR for season-pack matching. | `HDR10+` matches `HDR10` |
| Simplify WEB source matching | Off | Treat WEB-DL as WEB for season-pack matching. | `WEB-DL` matches `WEB` |
| Ignore year differences | Off | Allow matches when release dates differ or one side omits the year. | `Show.2024.S01E01` matches `Show.2025.S01E01` |

### Alternate titles

When Sonarr resolves the show, qui pulls its series-wide alternate titles and uses them during season-pack matching. This lets a pack and a local episode match when they use different title forms, such as a romaji pack against English-titled episodes, or an abbreviated title against the full one. Season-scoped alternate titles only apply to their own season, so they cannot bridge one season's episodes onto another season's pack. If the pack itself is titled by an alias that Sonarr maps to a different season than the pack labels (for example a `Show 2nd Season` pack labeled `S01`), alias expansion is skipped for that pack, since its numbering follows the alias rather than the canonical series. Without Sonarr knowing the show, only the literal parsed titles are compared.

### Anime absolute numbering

Absolute-numbered anime episodes (for example `Show - 1140`, with no season number) are matched against a seasoned pack when both sides use the same absolute numbering. qui keys the local episode to the pack's season and relies on the absolute episode number to identify it.

This does not translate between numbering schemes. A pack that uses `SxxExx` numbering will not match local episodes numbered by absolute number (or the reverse), since resolving one to the other needs authoritative per-episode data from a metadata provider. In practice releases that cross-seed cleanly already share a numbering convention.

One caveat: a `/check` call without torrent data has no file list to learn the pack's numbering scheme from, so it stays optimistic and can report ready against locals that use the other scheme. `/apply` verifies against the pack's real file list and is the authority; a false ready from the light check costs one wasted `.torrent` download, nothing is injected.

## Apply Model

Passing the threshold does **not** require 100% local coverage.

When `/apply` runs, qui:

- Links every matched episode file it can verify locally
- Leaves unmatched episodes and extras for qBittorrent to download
- Adds the torrent paused when anything is still missing
- Attempts an automatic recheck so qBittorrent can discover the linked bytes
- Queues automatic resume after recheck. qui resumes the torrent when qBittorrent reports progress at or above the configured season-pack coverage threshold, so qBittorrent can download the remaining files or pieces. If recheck finishes below that threshold, qui leaves the torrent paused for manual review.

If automatic recheck or resume queueing cannot be started, qui reports `automatic recheck failed`, `automatic resume is unavailable`, or `automatic resume queue is full`.

If **Skip Recheck** is enabled and the pack is incomplete, qui skips the apply instead of adding a broken torrent.

In hardlink mode, incomplete packs are also subject to piece-boundary protection. If pending files share torrent pieces with linked episode files, qui blocks the apply unless **Skip piece boundary safety check** is enabled. Reflink mode avoids that hardlink corruption risk because qBittorrent writes to cloned files instead of the original seeded files.

## Prerequisites

- **Local filesystem access** must be enabled on the target instance
- **Hardlink or reflink mode** must be enabled on the target instance - season packs always use linked trees
- The instance's link-mode base directory must be configured and writable. In the current UI/API this is the same base-directory field used by hardlink/reflink mode.

Instances without local filesystem access or a link mode are skipped during eligibility checks.

See [Hardlink Mode](./hardlink-mode.md) for setup instructions.

## Setup

### 1. Enable Season Packs in qui

- Go to **Cross-Seed > Rules > Season packs**
- Enable the feature
- Set the coverage threshold (default 75%)
- Optionally, add a TVDB API key for improved episode count accuracy. TVMaze is used automatically as a free fallback without any configuration.
- Optionally, configure **Category routing** for season pack injects. Add rules that map a resolution (and optionally a source) to a qBittorrent category, then set an **Anything else** fallback category for packs that match no rule. If you run multiple Sonarr instances, point each rule at the category that Sonarr watches on its qBittorrent download client: route `1080p` to `tv-hd` and `2160p` to `tv-uhd`, for example. Sonarr will pick up the assembled pack and hardlink-import its files into your library, so the same on-disk bytes back both the library and every seeded episode. Categories are created on demand when they do not exist yet, and existing categories are used untouched. If no rule matches and no fallback is set, season packs use the global Category Mode configured under **Cross-Seed > Rules > Categories**.

#### Category routing

Each routing rule matches on a resolution and an optional source:

| Field | Values | Effect |
| --- | --- | --- |
| Resolution | `2160p`, `1080p`, `720p`, `576p`, `480p` | Required. The pack resolution the rule applies to. |
| Source | Any, `WEB`, `BluRay`, `Remux`, `HDTV` | Optional. Restricts the rule to a single source, or leave as **Any** to match every source at that resolution. |
| Category | A qBittorrent category | Where matching packs are filed. Created on demand if it does not exist. |

When more than one rule could match a pack, the most specific rule wins: a rule with an explicit source beats an **Any**-source rule at the same resolution. If no rule matches, qui uses the **Anything else** fallback category.

:::tip
**Remux** is detected from the release tags, not the source field. A BluRay remux carries the remux tag, so it routes under the **Remux** option rather than the **BluRay** option. Add a separate `Remux` rule when you want remuxes filed away from regular BluRay packs.
:::

### 2. Create an API Key

If you don't already have one for autobrr:

- Go to **Settings > API Keys**
- Click **Create API Key**
- Copy the generated key

### 3. Configure autobrr External Filter

:::important
Create a **separate autobrr filter** for season packs. Do not reuse your existing cross-seed filter - the endpoints and payload are different.
:::

:::tip
**Docker Compose:** use your qui container hostname instead of `localhost` (often the Compose service name), for example: `http://qui:7476/api/cross-seed/season-pack/check`.
:::

In your new autobrr filter, go to **External** tab > **Add new**:

| Field                     | Value                                                     |
| ------------------------- | --------------------------------------------------------- |
| Type                      | `Webhook`                                                 |
| Name                      | `qui season pack`                                         |
| On Error                  | `Reject`                                                  |
| Endpoint                  | `http://localhost:7476/api/cross-seed/season-pack/check`  |
| HTTP Method               | `POST`                                                    |
| HTTP Request Headers      | `X-API-Key=YOUR_QUI_API_KEY`                              |
| Expected HTTP Status Code | `200`                                                     |

**Data (JSON):**

```json
{
  "torrentName": {{ toRawJson .TorrentName }},
  "instanceIds": [1],
  "indexer": {{ toRawJson .Indexer }}
}
```

To search all instances, omit `instanceIds`:

```json
{
  "torrentName": {{ toRawJson .TorrentName }},
  "indexer": {{ toRawJson .Indexer }}
}
```

:::tip
The check endpoint does not require the torrent file. Sending only the release name avoids downloading the `.torrent` for every season pack announce. qui uses Sonarr, TVDB, or TVMaze to determine the episode count for threshold enforcement. To include the torrent file in the check request, add `"torrentData": "{{ .TorrentDataRawBytes | toString | b64enc }}"` to the payload.
:::

**Field descriptions:**

- `torrentName` (required) - The release name as announced
- `torrentData` (optional) - Base64-encoded torrent file. When provided, qui parses it to determine playable pack files. When omitted, qui uses metadata providers for episode counts.
- `instanceIds` (optional) - qBittorrent instance IDs to scan. Omit to search all eligible instances.
- `indexer` (optional) - autobrr indexer identifier. Used when **Use indexer name as category** is enabled.

### 4. Configure the Apply Action

When `/check` returns `200 OK`, send the torrent to `/api/cross-seed/season-pack/apply`:

**Action setup in autobrr:**

| Field       | Value                                                                            |
| ----------- | -------------------------------------------------------------------------------- |
| Action Type | `Webhook`                                                                        |
| Name        | `qui season pack apply`                                                          |
| Endpoint    | `http://localhost:7476/api/cross-seed/season-pack/apply?apikey=YOUR_QUI_API_KEY` |

**Payload (JSON):**

```json
{
  "torrentName": {{ toRawJson .TorrentName }},
  "torrentData": "{{ .TorrentDataRawBytes | toString | b64enc }}",
  "instanceIds": [1],
  "indexer": {{ toRawJson .Indexer }}
}
```

**Field descriptions:**

- `torrentName` (required) - The release name
- `torrentData` (required) - Base64-encoded torrent file
- `instanceIds` (optional) - Target instances (omit to apply to any matching instance)
- `indexer` (optional) - autobrr indexer identifier. Used when **Use indexer name as category** is enabled.

## API Endpoints

| Method | Path                                  | Description                |
| ------ | ------------------------------------- | -------------------------- |
| POST   | `/api/cross-seed/season-pack/check`   | Check if a pack can be assembled |
| POST   | `/api/cross-seed/season-pack/apply`   | Assemble and add the pack  |
| GET    | `/api/cross-seed/season-pack/runs`    | List recent activity       |

The `/runs` endpoint accepts an optional `limit` query parameter (default 20, max 200). qui keeps the most recent 200 season-pack runs and prunes older rows when new check/apply activity is recorded.

`/check` returns `404 Not Found` for expected skips such as below-threshold coverage, disabled season packs, non-season-pack releases, or no eligible instances. `/apply` returns `500 Internal Server Error` when the pack cannot be applied, including skipped recheck-required packs, layout mismatch, add failure, or operational failures while reading qBittorrent state.

## Added Torrent Behavior

When qui applies a season pack, it:

- Always adds the torrent with an explicit `savepath` pointing at the linked tree
- Applies the tags configured in **Cross-Seed > Rules > Season packs**
- Adds incomplete packs paused, then best-effort attempts automatic recheck and queues automatic resume. After recheck, qui resumes at or above the configured season-pack coverage threshold; below that threshold, the torrent stays paused for manual review.
- Resolves the category in this order:
  - The category from the matching **Category routing** rule under **Cross-Seed > Rules > Season packs**, choosing the most specific rule when several apply (an explicit-source rule beats an Any-source rule at the same resolution). Recommended for Sonarr integration so the pack lands in Sonarr's download-client category and inherits hardlink-aware imports
  - Otherwise the **Anything else** fallback category, if set
  - Otherwise the global cross-seed category rules: custom category if enabled, otherwise category affix mode if enabled, otherwise indexer-name category if enabled, otherwise inheriting the matched episode's category
- Creates the resolved category on the target instance if it does not already exist

## Instance Selection

When `instanceIds` is omitted or contains multiple instances:

1. qui filters to instances with local filesystem access and hardlink/reflink mode
2. Existing webhook source filters are applied
3. The instance with the highest coverage is selected
4. Ties are broken by highest matched episode count, then lowest instance ID

## Activity

Each check and apply request records a season-pack run. qui keeps the most recent 200 runs. Recent runs are shown in **Cross-Seed > Rules > Season packs**. The panel shows the torrent name, phase (`check` or `apply`), status, reason, message, selected instance, matched episodes, total episodes, coverage, link mode, and timestamp.

You can also query recent runs directly:

```bash
curl -H "X-API-Key: YOUR_QUI_API_KEY" "http://localhost:7476/api/cross-seed/season-pack/runs?limit=20"
```

## Debugging

Start with autobrr:

- A rejected check usually appears as `[external webhook status code] not matching: got 404 want: 200`
- That means qui answered the season-pack check but did not consider the release ready to apply
- Confirm the release used the season-pack filter, not the regular cross-seed filter

Then check qui:

- Open **Cross-Seed > Rules > Season packs** and find the recent row for the torrent name
- Check the phase (`check` or `apply`), status, reason, message, coverage, matched episodes, total episodes, selected instance, and link mode
- If the row is missing, autobrr probably did not reach qui or used the wrong endpoint/API key. You can confirm with `/api/cross-seed/season-pack/runs?limit=20`.

For deeper logs, set:

```toml
loglevel = 'DEBUG'
```

Look for messages containing the torrent name and these clues:

- `season pack: failed to resolve Sonarr season total` - Sonarr lookup failed, so qui fell back to metadata providers or skipped threshold enforcement
- `season pack: metadata provider lookup failed` - TVDB/TVMaze lookup failed
- `load cached torrents for instance` - qBittorrent cache lookup failed, so the check/apply is an operational failure
- `unsafe piece boundary with pending files` - hardlink mode blocked an incomplete pack for safety
- `torrent added paused; recheck queued` - qui added the pack and queued automatic resume
- `Recheck completed below threshold, torrent left paused for manual review` - qBittorrent rechecked below the configured season-pack coverage threshold

Use `TRACE` when you need field-level matching details. Then look for `[CROSSSEED-MATCH] Release filtered` entries to see which release field caused an episode to be rejected.
