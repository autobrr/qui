---
sidebar_position: 6
title: autobrr Integration
---

# autobrr Integration

qui integrates with autobrr through webhook endpoints, enabling real-time cross-seed detection when autobrr announces new releases.

## How It Works

1. autobrr sees a new release from a tracker
2. autobrr sends the torrent name, reported size, and indexer to `/api/cross-seed/webhook/check`
3. qui checks your qBittorrent instances without downloading the torrent file
4. qui responds with:
   - `200 OK` – matching torrent is complete and ready to cross-seed
   - `202 Accepted` – matching torrent exists but still downloading; retry later
   - `404 Not Found` – no matching torrent exists
5. On `200 OK`, autobrr sends the torrent file and original announcement name to `/api/cross-seed/apply`
6. qui reads the actual size from the torrent file and repeats the match against the current local torrents

## Setup

### 1. Create an API Key in qui

- Go to **Settings → API Keys**
- Click **Create API Key**
- Name it (e.g., "autobrr webhook")
- Copy the generated key

### 2. Configure autobrr External Filter

:::important
Create a **new autobrr filter dedicated to qui**.
:::

:::note
The **External** webhook (`/api/cross-seed/webhook/check`) only answers: "is this ready to cross-seed?" It does **not** add a torrent to qBittorrent.

You must also set up the **Action** in [Apply Endpoint](#apply-endpoint).
:::

:::tip
**Docker Compose:** if autobrr and qui are both containers, `localhost` inside autobrr is the autobrr container, not qui.

Use your qui container hostname instead (often the Compose service name), for example: `http://qui:7476/api/cross-seed/webhook/check`.
:::

In your new autobrr filter, go to **External** tab → **Add new**:

| Field                     | Value                                                |
| ------------------------- | ---------------------------------------------------- |
| Type                      | `Webhook`                                            |
| Name                      | `qui`                                                |
| On Error                  | `Reject`                                             |
| Endpoint                  | `http://localhost:7476/api/cross-seed/webhook/check` |
| HTTP Method               | `POST`                                               |
| HTTP Request Headers      | `X-API-Key=YOUR_QUI_API_KEY`                         |
| Expected HTTP Status Code | `200`                                                |

**Data (JSON):**

```json
{
  "torrentName": {{ toRawJson .TorrentName }},
  "size": {{ .Size }},
  "instanceIds": [1],
  "indexer": {{ toRawJson .Indexer }}
}
```

To search all instances, omit `instanceIds`:

```json
{
  "torrentName": {{ toRawJson .TorrentName }},
  "size": {{ .Size }},
  "indexer": {{ toRawJson .Indexer }}
}
```

**Field descriptions:**

- `torrentName` (required): The release name as announced
- `size` (optional): The size that autobrr already knows, in bytes. A missing value or `0` means that no size is available.
- `instanceIds` (optional): qBittorrent instance IDs to scan. Omit to search all instances.
- `indexer` (optional): autobrr indexer identifier (for example `hdb`). Required for qui's HDBits-specific missing-collection fallback on `/check`.
- `findIndividualEpisodes` (optional): Override the global episode matching setting

### How the size check works

The `.Size` value comes from the announcement or feed data that autobrr already has. This template does not download the torrent file.

The value can be exact, rounded, or `0`. A positive value that equals the local torrent size lets qui consider approved metadata differences.

A rounded or unequal value cannot approve that fallback. It can only pass the normal strict match within the configured size tolerance.

If the value is `0`, qui uses a narrow name-only preflight. This preflight can approve the one action download, but it cannot approve an add.

With **Skip recheck**, `/check` rejects title, season, episode, and split release-group fallbacks before autobrr downloads the torrent file.

### 3. Configure Retry Handling

Use autobrr's **Retry** block to handle `202 Accepted` responses:

- **Retry HTTP status code(s):** `202`
- **Maximum retry attempts:** `10`
- **Retry delay in seconds:** `4`

## Apply Endpoint

When `/check` returns `200 OK`, send the torrent to `/api/cross-seed/apply`:

**Action setup in autobrr:**

| Field       | Value                                                                |
| ----------- | -------------------------------------------------------------------- |
| Action Type | `Webhook`                                                            |
| Name        | `qui cross-seed`                                                     |
| Endpoint    | `http://localhost:7476/api/cross-seed/apply?apikey=YOUR_QUI_API_KEY` |

**Payload (JSON):**

```json
{
  "torrentData": "{{ .TorrentDataRawBytes | toString | b64enc }}",
  "torrentName": {{ toRawJson .TorrentName }},
  "instanceIds": [1],
  "indexer": {{ toRawJson .Indexer }}
}
```

**Field descriptions:**

- `torrentData` (required) - Base64-encoded torrent file bytes
- `torrentName` (optional) - The original announced name. Include it to use reported-size matching and detect metadata changes.
- `instanceIds` (optional) - Target instances (omit to apply to any matching instance)
- `indexer` (optional) - autobrr indexer identifier (for example `hdb`). When "Use indexer name as category" mode is enabled, qui uses this identifier value as the category; ignored otherwise
- `tags` (optional) - Override webhook tags from settings
- `category` (optional) - Override category. Takes precedence over `indexer`
- `startPaused` (optional) - Override whether torrents are added paused
- `skipIfExists` (optional) - Skip adding if the torrent already exists
- `findIndividualEpisodes` (optional) - Override the global episode matching setting

The action is the first intended torrent-file download in this flow. qui calculates the actual total from the torrent metadata.

qui then repeats the match against the current local sources. The original `torrentName` prevents downloaded metadata from gaining new matching authority.

Clients that omit `torrentName` keep the legacy strict apply behavior. These clients cannot use the new reported-size fallback.

qui can set `skip_checking=true` when it adds a torrent. This option skips only qBittorrent's automatic add-time check.

Title, season, episode, and split release-group fallbacks still require an explicit full piece check. qui keeps these torrents paused, starts the check, and resumes them only at 100%.

Soft differences, such as codec, source, HDR, edition, or one-sided checksum data, keep the normal fast path after all file and layout checks.

On the normal fast path, qui applies the [Max auto-start download](./rules.md#max-auto-start-download) rule.

The normal rule includes a 200 MiB exception when only ignorable files are missing. Torrents above the permitted limit stay paused for review.

No autobrr source change or extra scrape is required.

### Troubleshooting: autobrr matches, but nothing gets added to qBittorrent

Use this when autobrr shows the filter accepted the release (or your Discord notification fires), but you never see a new torrent in qBittorrent.

1. **Confirm you added the `/apply` Action**
   - The External webhook (`/check`) does not add torrents.
   - You need an autobrr **Action** (Webhook) that calls `/api/cross-seed/apply` (above).
2. **Fix Docker networking if you're using containers**
   - `http://localhost:7476/...` only works if autobrr can reach qui on its own `localhost`.
   - In Docker Compose, use the qui service hostname (example): `http://qui:7476/api/cross-seed/apply?apikey=...`.
3. **Double-check auth**
   - `/check`: header `X-API-Key=...`
   - `/apply`: query string `?apikey=...` (as shown in this guide)
4. **Verify qui can talk to qBittorrent**
   - qui UI: **Settings → Instances → Test Connection**
5. **Check paused torrents**
   - Cross-seeds are often added **paused**. Look in qBittorrent's paused list (and any cross-seed tag/category you configured).

If you still can't see why, jump to [Cross-Seed Troubleshooting](./troubleshooting.md).

## Webhook Source Filters

By default, the webhook endpoint scans **all** torrents on your instances when looking for matches. You can configure filters to exclude certain categories or tags from being matched:

- **Exclude Categories:** Skip torrents in specific categories (e.g., `cross-seed-link`)
- **Exclude Tags:** Skip torrents with specific tags (e.g., `no-cross-seed`)
- **Include Categories:** Only match against torrents in these categories (leave empty for all)
- **Include Tags:** Only match against torrents with these tags (leave empty for all)

This is useful when:

- You have a legacy cross-seed category that shouldn't be re-matched
- Certain content types should never be considered for cross-seeding
- You want to exclude torrents with specific metadata tags

:::note
Exclude filters take precedence over include filters. Tag matching is case-sensitive. When both category and tag include filters are configured, a torrent must pass both filter checks (matching at least one allowed category AND at least one allowed tag).
:::

Configure in qui UI: **Cross-Seed → Auto → Webhook / autobrr**

## Season Pack Webhook

qui also supports a dedicated season-pack flow through separate endpoints. When autobrr announces a season pack, qui checks your instances for matching individual episodes, links whatever is already local, and lets qBittorrent fetch the remainder after recheck when coverage is sufficient.

This uses different endpoints (`/api/cross-seed/season-pack/check` and `/api/cross-seed/season-pack/apply`) and requires a separate autobrr filter.

See [Season Packs](./season-packs.md) for full setup instructions.
