---
sidebar_position: 7
title: Reverse Proxy
description: Let external apps access qBittorrent through qui without credentials.
---

# Reverse proxy for external applications

qui includes a built-in reverse proxy. External applications such as autobrr, Sonarr, and Radarr connect to your qBittorrent instances through it without qBittorrent credentials.

## How it works

qui keeps a shared session with each qBittorrent instance and proxies requests from your external applications. The applications reuse the live session instead of logging in again.

## Setup instructions

### 1. Create a client API key

1. Open qui in your browser.
2. Go to **Settings → Client Proxy**.
3. Click **Create Client API Key**.
4. Enter a name for the client (for example, "Sonarr").
5. Select the qBittorrent instance that you want to proxy.
6. Click **Create Client API Key**.
7. Copy the generated proxy URL now. qui shows it only once.

The **Client Proxy** page lists every key with its client name, instance, creation date, and last-used date.

### 2. Configure your external application

Use qui as the qBittorrent host with the proxy URL format:

**Complete URL example:**
```text
http://localhost:7476/proxy/abc123def456ghi789jkl012mno345pqr678stu901vwx234yz
```

## Application-specific setup

### Sonarr / Radarr

1. Go to `Settings → Download Clients`.
2. Select `Show Advanced`.
3. Add a new **qBittorrent** client.
4. Set the host and port of qui.
5. Set URL Base to the `/proxy/...` path. If you use a custom base URL, include it (for example, `/qui/`).
6. Click **Test**. If the test succeeds, click **Save**.

### autobrr

1. Open `Settings → Download Clients`.
2. Add **qBittorrent** (or edit an existing client).
3. Enter the full URL, for example: `http://localhost:7476/proxy/abc123def456ghi789jkl012mno345pqr678stu901vwx234yz`
4. Leave username and password blank and click **Test**.
5. Leave basic auth blank. qui handles authentication.

If you use cross-seed integration with autobrr, see the [Cross-Seed](./cross-seed/autobrr.md) section.

### cross-seed

1. Open the cross-seed configuration file.
2. Add or edit the `torrentClients` section.
3. Append the full URL as the documentation describes:
   ```js
   torrentClients: ["qbittorrent:http://localhost:7476/proxy/abc123def456ghi789jkl012mno345pqr678stu901vwx234yz"],
   ```
4. Save the configuration file and restart cross-seed.

### Upload Assistant

1. Open the Upload Assistant configuration file.
2. Add or edit `qui_proxy_url` under the qBitTorrent client settings.
3. Append the full URL, for example: `"qui_proxy_url": "http://localhost:7476/proxy/abc123def456ghi789jkl012mno345pqr678stu901vwx234yz",`
4. Leave all other authentication settings unchanged.
5. Save the configuration file.

## Supported applications

The reverse proxy works with any application that supports qBittorrent's Web API.

## Security features

- **API key authentication**: each client needs its own key.
- **Instance isolation**: a key grants access to one qBittorrent instance.
- **Usage tracking**: qui records when clients last access each key.
- **Revocation**: delete a key to remove access.
- **No credential exposure**: qBittorrent passwords never leave qui.

## Intercepted endpoints

The proxy intercepts some qBittorrent API endpoints to improve performance and add qui-specific behavior. qui forwards most requests unchanged to qBittorrent.

### Read operations (served from qui)

qui serves these endpoints from its sync manager for faster responses:

| Endpoint | Description |
|----------|-------------|
| `/api/v2/torrents/info` | Torrent list with standard qBittorrent filtering |
| `/api/v2/torrents/search` | Torrent list with fuzzy search (qui-specific) |
| `/api/v2/torrents/categories` | Category list from synchronized data |
| `/api/v2/torrents/tags` | Tag list from synchronized data |
| `/api/v2/torrents/properties` | Torrent properties |
| `/api/v2/torrents/trackers` | Torrent trackers, with icon discovery |
| `/api/v2/torrents/files` | Torrent file list |
| `/api/v2/torrents/mediainfo` | MediaInfo report for a file on disk (qui-specific, needs **Local Filesystem Access**) |

These endpoints proxy to qBittorrent and update qui's local state:

| Endpoint | Description |
|----------|-------------|
| `/api/v2/sync/maindata` | Full sync data (updates qui's cache) |
| `/api/v2/sync/torrentPeers` | Peer data (updates qui's peer state) |

### Write operations

| Endpoint | Behavior |
|----------|----------|
| `/api/v2/auth/login` | If the instance is healthy, returns success as a no-op |
| `/api/v2/torrents/reannounce` | If tracker monitoring is enabled, routes to qui's reannounce service |
| `/api/v2/torrents/setLocation` | Forwards to qBittorrent, invalidates the file cache |
| `/api/v2/torrents/renameFile` | Forwards to qBittorrent, invalidates the file cache |
| `/api/v2/torrents/renameFolder` | Forwards to qBittorrent, invalidates the file cache |
| `/api/v2/torrents/delete` | Forwards to qBittorrent, invalidates the file cache |

qui forwards all other endpoints unchanged to qBittorrent.
