---
sidebar_position: 7
title: Season Packs
description: Assemble season packs from individual episodes, through autobrr webhooks or automatic cross-seed assembly.
---

# Season Packs

qui assembles season-pack torrents from individual episodes that you already seed. When autobrr announces a season pack, qui checks your qBittorrent instances for completed, release-compatible episodes. If enough local data exists, qui builds a linked directory tree, adds the torrent, and lets qBittorrent download any missing files.

The webhook flow below is one of two triggers. The second trigger is [Automatic Assembly](#automatic-assembly), which requires no autobrr filter.

## How it works

1. autobrr sees a season pack release.
2. autobrr sends the torrent name (and optionally the torrent file) to the qui `/api/cross-seed/season-pack/check` endpoint.
3. If you provide a torrent file, qui parses its file list to determine the playable episode files. If not, qui queries metadata providers for episode counts.
4. qui scans your qBittorrent instances for completed individual episodes that match the release details of the season pack.
5. qui computes coverage from completed, matching local episodes:
   - If the request includes torrent data, the playable episode files in the pack torrent define the expected pack layout.
   - If Sonarr can resolve the show, qui asks Sonarr for the season total first.
   - If Sonarr cannot resolve the show, qui falls back to metadata providers: TVDB when configured, then TVMaze.
   - If the request includes torrent data, qui never uses a total lower than the playable episode count inside the pack torrent.
6. qui responds with:
   - `200 OK`: coverage meets the threshold, ready to apply
   - `404 Not Found`: local coverage is too low, the release is not a season pack, or the feature is disabled
7. If qui returns `200 OK`, autobrr sends the torrent file to `/api/cross-seed/season-pack/apply`.
8. qui links the matched episodes, applies your configured season-pack tags, and adds the season pack torrent. qui never links a local episode file if its size or release details differ from the pack file. qui treats that episode as missing and downloads it instead. If these demotions drop coverage below the threshold, the apply fails as `drifted`.
9. If episodes or extras are missing, qui adds the torrent paused, attempts an automatic recheck, and queues automatic resume. After the recheck, qui resumes the torrent when qBittorrent confirms the linked bytes. qBittorrent then downloads the missing files. If the recheck reports far fewer bytes than the linked bytes, some links are invalid, and qui leaves the torrent paused for manual review. qui reports best-effort fallbacks by name, including `automatic recheck failed`, `automatic resume is unavailable`, and `automatic resume queue is full`.

## Automatic Assembly

qui also assembles season packs without autobrr webhooks. To turn this on, enable **Assemble season packs automatically** in **Cross-Seed > Rules > Season packs**. The switch is off by default and operates independently of the webhook feature.

If the switch is on, qui diverts a season pack into the assembly pipeline when all of these conditions are true:

- RSS automation, an autobrr cross-seed action, or a manual apply sends the season pack to cross-seed
- You seed episodes of the same show and season
- No direct cross-seed match applies

The diverted pack then goes through the same steps as a webhook apply: qui computes coverage, links the matched episodes, and lets qBittorrent download the rest.

Automatic assembly uses the same settings as the webhook flow: the coverage threshold, the matching settings, category routing, tags, and metadata providers. qui records each attempt in the [Activity](#activity) panel.

:::note
Automatic assembly also extends library search runs. If a show has three or more seeded episodes of one season and no seeded pack, the search run queries your indexers for the season pack and assembles a match through the same pipeline. If you want to search for season packs without single-episode searches, enable **Skip individual episodes** in the Library Scan form.
:::

## Coverage model

qui uses a provider-first episode total with the pack torrent as the layout source.

For `/check` without torrent data:

- qui asks Sonarr for the season episode total first
- If Sonarr fails or cannot resolve the show, qui asks TVDB when configured, or TVMaze
- If no provider returns a total, qui skips threshold enforcement and only verifies that matching episodes exist

For `/check` or `/apply` with torrent data:

- The torrent file is the source of truth for the pack layout and playable episode files
- qui still asks Sonarr, then TVDB or TVMaze, for a season total
- If the provider total is lower than the playable episode count in the torrent, qui uses the playable file count instead

The apply endpoint always requires the torrent file and enforces the threshold.

If qui falls back to the pack torrent, it:

- Counts only playable video files (mkv, mp4, avi, and more)
- Ignores subtitles, NFOs, samples, and other extras
- Deduplicates episodes that appear more than once
- Rejects packs with zero usable episode files

Coverage is then: `matchedLocalEpisodes / coverageTotalEpisodes`

For an episode to count toward coverage, it must:

- Be fully downloaded (`100%` progress)
- Pass the same release-compatibility checks used by normal cross-seeding
- Belong to the same episode in the season pack

Mixed variants do **not** count toward coverage. For example, `720p WEB` episodes do not satisfy a `1080p BluRay` season pack.

The default threshold is **75%**. Change it in **Cross-Seed > Rules > Season packs** in the qui UI.

## Matching settings

These settings affect only season-pack checks and applies. They do not change normal cross-seed matching in the Rules tab.

The defaults match common seasonpackarr expectations.

| Setting | Default | Effect | Example |
| --- | --- | --- | --- |
| Ignore REPACK/PROPER differences | On | Treat REPACK and PROPER episodes as compatible with the season pack. | `Show.S01E01.REPACK` matches `Show.S01E01.PROPER` |
| Simplify HDR matching | Off | Treat HDR10, HDR10+, and HDR+ as HDR for season-pack matching. | `HDR10+` matches `HDR10` |
| Simplify WEB source matching | Off | Treat WEB-DL as WEB for season-pack matching. | `WEB-DL` matches `WEB` |
| Ignore year differences | Off | Allow matches when release dates differ or when one side omits the year. | `Show.2024.S01E01` matches `Show.2025.S01E01` |

### Alternate titles

If Sonarr resolves the show, qui pulls its series-wide alternate titles and uses them during season-pack matching. This allows a pack and a local episode to match when they use different title forms, such as a romaji pack against English-titled episodes, or an abbreviated title against the full title. Season-scoped alternate titles apply only to their own season, so they cannot bridge one season's episodes onto another season's pack. If the pack title uses an alias that Sonarr maps to a different season than the pack labels (for example, a `Show 2nd Season` pack labeled `S01`), qui skips alias expansion for that pack. Its numbering follows the alias rather than the canonical series. If Sonarr does not recognize the show, qui compares only the literal parsed titles.

### Anime absolute numbering

If both sides use the same absolute numbering, qui matches absolute-numbered anime episodes (for example `Show - 1140`, with no season number) against a season pack. qui keys the local episode to the pack season and uses the absolute episode number to identify it.

This feature does not translate between different numbering schemes. A pack that uses `SxxExx` numbering does not match local episodes numbered with absolute numbers, and the reverse also holds. Translation between schemes requires authoritative per-episode data from a metadata provider. In practice, releases that cross-seed cleanly already share a numbering convention.

A `/check` call without torrent data has no file list to determine the pack's numbering scheme. The check remains optimistic and reports ready against local episodes that use the other scheme. The `/apply` endpoint verifies the real file list of the pack and acts as the authority. A false ready result from the light check costs only one wasted `.torrent` download, and qui injects nothing.

## Apply model

Passing the threshold does **not** require 100% local coverage.

When `/apply` runs, qui:

- Links every matched episode file that it verifies locally
- Leaves unmatched episodes and extras for qBittorrent to download
- Adds the torrent paused if any files are missing
- Attempts an automatic recheck so qBittorrent discovers the linked bytes
- Queues automatic resume after the recheck. After the recheck, qui resumes the torrent when qBittorrent confirms the linked bytes, which allows qBittorrent to download the remaining files or pieces. If the recheck reports far fewer bytes than the linked bytes, some links are invalid, and qui leaves the torrent paused for manual review.

If automatic recheck or resume queueing cannot start, qui reports `automatic recheck failed`, `automatic resume is unavailable`, or `automatic resume queue is full`.

If **Skip Recheck** is enabled and the pack is incomplete, qui skips the apply instead of adding a broken torrent.

In hardlink mode, qui can also apply piece-boundary protection to incomplete packs. If pending files share torrent pieces with linked episode files and the **Piece boundary safety check** in **Cross-Seed > Rules > Safety & validation** is enabled, qui blocks the apply. This check is off by default. Reflink mode avoids that hardlink corruption risk because qBittorrent writes to cloned files instead of the original seeded files.

## Prerequisites

- You must enable **Local filesystem access** on the target instance
- You must enable **Hardlink or reflink mode** on the target instance. Season packs always use linked trees.
- You must configure a writable link-mode base directory for the instance. In the current UI/API, this is the same base directory field used by hardlink/reflink mode.

During eligibility checks, qui skips instances that lack local filesystem access or a link mode.

See [Hardlink Mode](./hardlink-mode.md) for setup instructions.

## Setup

### 1. Enable season packs in qui

- Go to **Cross-Seed > Rules > Season packs**
- Enable the feature
- Set the coverage threshold (default 75%)
- Optionally, add a TVDB API key for better episode count accuracy. qui uses TVMaze as a free fallback without any configuration.
- Optionally, configure **Category routing** for season pack injects. Add rules that map a resolution (and optionally a source) to a qBittorrent category. Then set an **Anything else** fallback category for packs that match no rule. If you run multiple Sonarr instances, point each rule to the category that Sonarr monitors on its qBittorrent download client (for example, route `1080p` to `tv-hd` and `2160p` to `tv-uhd`). Sonarr picks up the assembled pack and imports it. If Sonarr uses hardlinks and the library sits on the same filesystem, the same on-disk bytes back both the library and every seeded episode. If a category does not exist yet, qui creates it on demand. qui leaves existing categories untouched. If no rule matches and you set no fallback, season packs use the global Category Mode configured under **Cross-Seed > Rules > Categories**.

#### Category routing

Each routing rule matches on a resolution and an optional source:

| Field | Values | Effect |
| --- | --- | --- |
| Resolution | `2160p`, `1080p`, `720p`, `576p`, `480p` | Required. The pack resolution that the rule targets. |
| Source | Any, `WEB`, `BluRay`, `Remux`, `HDTV` | Optional. Restricts the rule to a single source. Leave as **Any** to match every source at that resolution. |
| Category | A qBittorrent category | The destination category for matching packs. qui creates the category on demand if it does not exist. |

If multiple rules match a pack, the most specific rule wins. An explicit-source rule takes precedence over an **Any**-source rule at the same resolution. If no rule matches, qui uses the **Anything else** fallback category.

:::tip
qui detects **Remux** from the release tags, not from the source field. A BluRay remux carries the remux tag, so qui routes it under the **Remux** option rather than the **BluRay** option. If you want to separate remuxes from regular BluRay packs, add a dedicated `Remux` rule.
:::

### 2. Create an API key

If you do not already have an API key for autobrr:

- Go to **Settings > API Keys**
- Click **Create API Key**
- Copy the generated key

### 3. Configure the autobrr external filter

:::warning
Create a **separate autobrr filter** for season packs. Do not reuse your existing cross-seed filter. The endpoints and payload differ.
:::

Set the filter priorities in this order:

1. Your regular [qui cross-seed filter](https://autobrr.com/filters/cross-seed-qui#create-the-filter)
2. The season-pack filter from this guide
3. Your normal TV or Sonarr grab filters

autobrr stops after the first successful matching filter. This order lets a direct whole-pack cross-seed win first. If qui rejects the season-pack check, autobrr can continue to a normal grab filter.

In the **Movies and TV** tab, use the [season-pack matcher](https://autobrr.com/filters/examples#only-season-packs):

| Field    | Value  |
| -------- | ------ |
| Seasons  | `1-99` |
| Episodes | `0`    |

This includes ordinary `S01` through `S99` packs and excludes specials (`S00`), seasonless packs, and individual episodes.

:::tip
**Docker Compose:** Use your qui container hostname instead of `localhost` (often the Compose service name), for example: `http://qui:7476/api/cross-seed/season-pack/check`.
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

Leave every field under **Retry** blank. The grey `10` and `1` values are placeholders, not active defaults. A blank retry section makes one check request. The endpoint uses `404` as a normal "not eligible" result, so retrying it does not help.

**Data (JSON):**

```json
{
  "torrentName": {{ toRawJson .TorrentName }},
  "instanceIds": [1],
  "indexer": {{ toRawJson .Indexer }}
}
```

If you want to search all instances, omit `instanceIds`:

```json
{
  "torrentName": {{ toRawJson .TorrentName }},
  "indexer": {{ toRawJson .Indexer }}
}
```

:::tip
The check endpoint does not require the torrent file. Sending only the release name avoids downloading the `.torrent` for every season pack announce. qui uses Sonarr, TVDB, or TVMaze to determine the episode count for threshold enforcement. If you want to include the torrent file in the check request, add `"torrentData": "{{ .TorrentDataRawBytes | toString | b64enc }}"` to the payload.
:::

**Field descriptions:**

- `torrentName` (required): the release name as announced
- `torrentData` (optional): Base64-encoded torrent file. If you provide this field, qui parses the file to determine the playable pack files. If you omit it, qui uses metadata providers for episode counts.
- `instanceIds` (optional): qBittorrent instance IDs to scan. Omit this field to search all eligible instances.
- `indexer` (optional): autobrr indexer identifier. qui uses this when you enable **Use indexer name as category**.

### 4. Configure the apply action

If `/check` returns `200 OK`, send the torrent to `/api/cross-seed/season-pack/apply`:

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

- `torrentName` (required): the release name
- `torrentData` (required): Base64-encoded torrent file
- `instanceIds` (optional): target instances (omit to apply to any matching instance)
- `indexer` (optional): autobrr indexer identifier. qui uses this when you enable **Use indexer name as category**.

## API endpoints

| Method | Path                                  | Description                |
| ------ | ------------------------------------- | -------------------------- |
| POST   | `/api/cross-seed/season-pack/check`   | Check if a pack can be assembled |
| POST   | `/api/cross-seed/season-pack/apply`   | Assemble and add the pack  |
| GET    | `/api/cross-seed/season-pack/runs`    | List recent activity       |

The `/runs` endpoint accepts an optional `limit` query parameter (default 20, max 200). qui stores the most recent 200 season-pack runs and prunes older rows when it records new check or apply activity.

`/check` returns `404 Not Found` for expected skips, such as below-threshold coverage, disabled season packs, non-season-pack releases, or missing eligible instances. `/apply` returns `500 Internal Server Error` if qui cannot apply the pack. Reasons include skipped recheck-required packs, layout mismatches, torrent add failures, or operational errors while qui reads qBittorrent state.

## Added torrent behavior

When qui applies a season pack, it:

- Always adds the torrent with an explicit `savepath` that points to the linked tree
- Applies the **Season pack tags** configured in **Cross-Seed > Rules > Tagging**
- Adds incomplete packs in a paused state, attempts an automatic recheck, and queues automatic resume on a best-effort basis. After the recheck, qui resumes the torrent when qBittorrent confirms the linked bytes. If the recheck reports far fewer bytes, the torrent remains paused for manual review.
- Resolves the category in this order:
  - The category from the matching **Category routing** rule under **Cross-Seed > Rules > Season packs**. If multiple rules apply, the most specific rule wins (an explicit-source rule beats an Any-source rule at the same resolution). This configuration integrates with Sonarr so that the pack lands in Sonarr's download-client category and uses hardlink-aware imports.
  - The **Anything else** fallback category, if set.
  - The global cross-seed category rules (custom category if enabled, otherwise category affix mode if enabled, otherwise indexer-name category if enabled, otherwise the category of the matched episode).
- Creates the resolved category on the target instance if the category does not exist.

## Instance selection

If `instanceIds` is omitted or contains multiple instances:

1. qui filters the list to instances with local filesystem access and hardlink or reflink mode
2. Inside each instance, qui counts only the episode torrents that pass the webhook source filters (categories and tags)
3. qui selects the instance with the highest coverage
4. qui breaks ties by highest matched episode count, then lowest instance ID

## Activity

Each check request, apply request, and automatic assembly attempt records a season-pack run. qui stores the most recent 200 runs. Recent runs appear in **Cross-Seed > Rules > Season packs**. The panel displays the torrent name, phase (`check` or `apply`), status, reason, message, selected instance, matched episodes, total episodes, coverage, link mode, and timestamp.

You can also query recent runs directly:

```bash
curl -H "X-API-Key: YOUR_QUI_API_KEY" "http://localhost:7476/api/cross-seed/season-pack/runs?limit=20"
```

## Debugging

Start with autobrr:

- A rejected check usually appears as `[external webhook status code] not matching: got 404 want: 200`
- This status means qui processed the season-pack check, but the release was not ready to apply.
- Verify that the release matched the season-pack filter and not the standard cross-seed filter.

Then check qui:

- Open **Cross-Seed > Rules > Season packs** and locate the row for the torrent name.
- Check the phase (`check` or `apply`), status, reason, message, coverage, matched episodes, total episodes, selected instance, and link mode.
- If the row is missing, autobrr failed to reach qui or used the wrong endpoint or API key. Confirm this with `/api/cross-seed/season-pack/runs?limit=20`.

For deeper logs, set:

```toml
loglevel = 'DEBUG'
```

Look for log messages that contain the torrent name and these phrases:

- `season pack: failed to resolve Sonarr season total`: The Sonarr lookup failed, so qui fell back to metadata providers or skipped threshold enforcement.
- `season pack: metadata provider lookup failed`: The TVDB or TVMaze lookup failed.
- `load cached torrents for instance`: The qBittorrent cache lookup failed, causing the check or apply operation to fail.
- `unsafe piece boundary with pending files`: Hardlink mode blocked an incomplete pack for safety.
- `torrent added paused; recheck queued`: qui added the pack and queued automatic resume.
- `Recheck completed below threshold, torrent left paused for manual review`: The recheck reported fewer bytes than qui linked, indicating bad links.

qui logs field-level matching details at `DEBUG`, which is the default level. If you upgraded from an older version, open `config.toml`. If `logLevel` holds another value, set it to `DEBUG`. Then look for `[CROSSSEED-MATCH] Release filtered` entries. Each entry names the release field that did not match.
