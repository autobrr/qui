---
sidebar_position: 7
title: Season Packs
description: Assemble season packs from individual episodes using autobrr webhooks.
---

# Season Packs

qui can assemble season-pack torrents from individual episodes you already seed. When autobrr announces a season pack, qui checks your qBittorrent instances for completed, release-compatible episodes and, if enough local data is present, builds a linked directory tree, adds the torrent, and lets qBittorrent download anything still missing.

## How It Works

1. autobrr sees a season pack release
2. autobrr sends the torrent name and torrent file to qui's `/api/cross-seed/season-pack/check` endpoint
3. qui parses the pack torrent's file list to determine playable episode files
4. qui scans your qBittorrent instances for completed individual episodes that match the season pack's release details
5. qui computes coverage using the larger of:
   - Sonarr's season episode total, when Sonarr can resolve the show
   - The playable episode count inside the pack torrent
6. qui responds with:
   - `200 OK` - coverage meets the threshold, ready to apply
   - `404 Not Found` - local coverage is too low, the release is not a season pack, or the feature is disabled
7. On `200 OK`, autobrr sends the torrent file to `/api/cross-seed/season-pack/apply`
8. qui links the matched episodes, applies your configured season-pack tags, and adds the season pack torrent
9. If episodes or extras are still missing, qui adds the torrent paused, triggers a recheck, then auto-resumes once qBittorrent reports the linked data

## Coverage Model

qui uses the larger of:

- Sonarr's episode count for the matched season, when Sonarr can resolve the release
- The playable episode count inside the pack torrent

The torrent file still provides the file-layout truth for apply. Sonarr only improves the threshold decision.

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

The default threshold is **75%**. Change it in **Cross-Seed > Season Packs** in the qui UI.

## Apply Model

Passing the threshold does **not** require 100% local coverage.

When `/apply` runs, qui:

- Links every matched episode file it can verify locally
- Leaves unmatched episodes and extras for qBittorrent to download
- Adds the torrent paused when anything is still missing
- Triggers recheck so qBittorrent discovers the linked bytes
- Auto-resumes once recheck finishes at the expected local-data threshold

If **Skip Recheck** is enabled and the pack is incomplete, qui skips the apply instead of adding a broken torrent.

## Prerequisites

- **qBittorrent only** - other clients are not supported in v1
- **Local filesystem access** must be enabled on the target instance
- **Hardlink or reflink mode** must be enabled on the target instance - season packs always use linked trees

Instances without local filesystem access or a link mode are skipped during eligibility checks.

See [Hardlink Mode](hardlink-mode) for setup instructions.

## Setup

### 1. Enable Season Packs in qui

- Go to **Cross-Seed > Season Packs**
- Enable the feature
- Set the coverage threshold (default 75%)

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
  "torrentData": "{{ .TorrentDataRawBytes | toString | b64enc }}",
  "instanceIds": [1],
  "indexer": {{ toRawJson .Indexer }}
}
```

To search all instances, omit `instanceIds`:

```json
{
  "torrentName": {{ toRawJson .TorrentName }},
  "torrentData": "{{ .TorrentDataRawBytes | toString | b64enc }}",
  "indexer": {{ toRawJson .Indexer }}
}
```

**Field descriptions:**

- `torrentName` (required) - The release name as announced
- `torrentData` (required) - Base64-encoded torrent file. qui parses this to determine playable pack files and apply layout.
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

The `/runs` endpoint accepts an optional `limit` query parameter (default 20, max 200).

## Added Torrent Behavior

When qui applies a season pack, it:

- Always adds the torrent with an explicit `savepath` pointing at the linked tree
- Applies the tags configured in **Cross-Seed > Season Packs**
- Adds incomplete packs paused, triggers recheck, then auto-resumes after qBittorrent discovers the linked files
- Uses your normal cross-seed category rules:
  - Custom category, if enabled
  - Otherwise category affix mode, if enabled
  - Otherwise indexer-name category, if enabled

## Instance Selection

When `instanceIds` is omitted or contains multiple instances:

1. qui filters to instances with local filesystem access and hardlink/reflink mode
2. Existing webhook source filters are applied
3. The instance with the highest coverage is selected
4. Ties are broken by highest matched episode count, then lowest instance ID

## Activity

Recent season pack activity is visible in the **Cross-Seed > Season Packs** tab. Each check and apply request creates one activity row showing the torrent name, coverage, status, and selected instance.
