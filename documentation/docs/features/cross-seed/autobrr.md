---
sidebar_position: 6
title: autobrr Integration
description: Send autobrr announces to the qui cross-seed webhook.
---

# autobrr Integration

qui integrates with autobrr through webhook endpoints. When autobrr announces a new release, qui checks for a cross-seed match in real time.

## How It Works

1. autobrr sees a new release from a tracker
2. autobrr sends the torrent name, reported size, and indexer to `/api/cross-seed/webhook/check`
3. qui checks your qBittorrent instances without downloading a torrent file
4. qui responds with:
   - `200 OK`: a matching torrent is complete and ready to cross-seed
   - `202 Accepted`: a matching torrent exists but is not complete yet. Retry later.
   - `404 Not Found`: no matching torrent exists
5. On `200 OK`, autobrr sends the torrent file and original announcement name to `/api/cross-seed/apply`
6. qui reads the actual size from the torrent file and repeats the match against the current local torrents

## Setup

### 1. Create an API Key in qui

- Go to **Settings → API Keys**
- Click **Create API Key**
- Name it (for example "autobrr webhook")
- Copy the generated key

### 2. Configure autobrr External Filter

:::warning
Create a **new autobrr filter dedicated to qui**.
:::

:::note
The **External** webhook (`/api/cross-seed/webhook/check`) only answers: "is this ready to cross-seed?" It does **not** add a torrent to qBittorrent.

You must also configure the **Action** in [Apply Endpoint](#apply-endpoint).
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
- `indexer` (optional): The autobrr indexer identifier (for example `hdb`). On `/check`, qui only writes it to the debug log. On `/apply`, qui uses it for the **Use indexer name as category** mode.
- `findIndividualEpisodes` (optional): Override the global episode matching setting

### How the size check works

The `.Size` value comes from the announcement or feed data that autobrr already has. This template does not download the torrent file.

The value can be exact, rounded, or `0`. If a positive value equals the local torrent size, qui considers approved metadata differences.

If the value is rounded or unequal, qui cannot approve that fallback. It only passes the normal strict match within the configured size tolerance.

If the value is `0`, qui uses a narrow name-only preflight. This preflight can approve the action download, but it cannot approve an add.

If you enable **Skip recheck**, `/check` rejects title, season, episode, and split release-group fallbacks before autobrr downloads the torrent file.

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

- `torrentData` (required): Base64-encoded torrent file bytes
- `torrentName` (optional): The original announced name. Include it to use reported-size matching and detect metadata changes.
- `instanceIds` (optional): Target instances (omit to apply to any matching instance)
- `indexer` (optional): The autobrr indexer identifier (for example `hdb`). If you enable "Use indexer name as category", qui uses this value as the category. Otherwise qui ignores it.
- `tags` (optional): Override the webhook tags from settings
- `category` (optional): Override the category. Takes precedence over `indexer`.
- `startPaused` (optional): Override whether qui adds torrents paused
- `skipIfExists` (optional): Skip the add if the torrent already exists
- `findIndividualEpisodes` (optional): Override the global episode matching setting

The action performs the first torrent-file download in this flow. qui calculates the actual total from the torrent metadata.

qui then repeats the match against the current local sources. The original `torrentName` prevents downloaded metadata from gaining new matching authority.

If a client omits `torrentName`, it keeps the legacy strict apply behavior. These clients cannot use the new reported-size fallback.

When qui adds a torrent, it can set `skip_checking=true`. This option skips only qBittorrent's automatic add-time check.

Title, season, episode, and split release-group fallbacks still require an explicit full piece check. qui keeps these torrents paused, starts the check, and resumes them only at 100%.

Soft differences, such as codec, source, HDR, edition, or one-sided checksum data, keep the normal fast path after all file and layout checks.

On the normal fast path, qui applies the [Max auto-start download](./rules.md#max-auto-start-download) rule.

If only ignorable files are missing, the normal rule includes a 200 MiB exception. Torrents above the permitted limit stay paused for review.

This flow needs no autobrr source change and no extra scrape.

### Troubleshooting: autobrr matches, but qBittorrent shows no new torrent

If autobrr shows that the filter accepted the release, or your autobrr notification fires, but qBittorrent shows no new torrent, use these steps:

1. **Make sure that you added the `/apply` Action**
   - The External webhook (`/check`) does not add torrents.
   - You need an autobrr **Action** (Webhook) that calls `/api/cross-seed/apply` (above).
2. **Fix Docker networking if you use containers**
   - If you use containers, `http://localhost:7476/...` works only when autobrr can reach qui on its own `localhost`.
   - If you use Docker Compose, use the qui service hostname, for example `http://qui:7476/api/cross-seed/apply?apikey=...`.
3. **Check the authentication**
   - `/check`: header `X-API-Key=...`
   - `/apply`: query string `?apikey=...` (as shown in this guide)
4. **Make sure that qui can reach qBittorrent**
   - qui UI: **Settings → Instances → Test Connection**
5. **Check paused torrents**
   - qui often adds cross-seeds **paused**. Look in qBittorrent's paused list and in any cross-seed tag or category you configured.

If the cause is still not clear, see [Cross-Seed Troubleshooting](./troubleshooting.md).

## Webhook Source Filters

By default, the webhook endpoint matches against **all** torrents on your instances. You can configure filters to exclude categories or tags from the match:

- **Exclude Categories:** Skip torrents in specific categories (for example `cross-seed-link`)
- **Exclude Tags:** Skip torrents with specific tags (for example `no-cross-seed`)
- **Include Categories:** Match only torrents in these categories (leave empty for all)
- **Include Tags:** Match only torrents with these tags (leave empty for all)

Use these filters when:

- You have a legacy cross-seed category that qui must not match again
- qui must never consider certain content types for cross-seeding
- You want to exclude torrents with specific metadata tags

:::note
Exclude filters take precedence over include filters. Tag matching is case-sensitive. If you configure both category and tag include filters, a torrent must pass both checks. It must match at least one allowed category and at least one allowed tag.
:::

Configure in qui UI: **Cross-Seed → Auto → Webhook / autobrr**

## Season Pack Webhook

qui also has a dedicated season-pack flow with separate endpoints. When autobrr announces a season pack, qui checks your instances for matching individual episodes. It links the episodes that are already local. When coverage is sufficient, qui adds the pack and qBittorrent fetches the remainder after a recheck.

This flow uses `/api/cross-seed/season-pack/check` and `/api/cross-seed/season-pack/apply`. It requires a separate autobrr filter.

See [Season Packs](./season-packs.md) for full setup instructions.
