---
sidebar_position: 6
title: Reverse Proxy
description: Let external apps access qBittorrent through qui without credentials.
---

# Reverse Proxy for External Applications

qui includes a built-in reverse proxy that allows external applications like autobrr, Sonarr, Radarr, and other tools to connect to your qBittorrent instances **without needing qBittorrent credentials**.

## How It Works

qui maintains a shared session with qBittorrent and proxies requests from your external apps. This eliminates login thrash - automation tools reuse the live session instead of racing to re-authenticate.

## Setup Instructions

### 1. Create a Client Proxy API Key

1. Open qui in your browser
2. Go to **Settings → Client Proxy Keys**
3. Click **"Create Client API Key"**
4. Enter a name for the client (e.g., "Sonarr")
5. Choose the qBittorrent instance you want to proxy
6. Click **"Create Client API Key"**
7. **Copy the generated proxy url immediately** - it's only shown once

### 2. Configure Your External Application

Use qui as the qBittorrent host with the special proxy URL format:

**Complete URL example:**
```
http://localhost:7476/proxy/abc123def456ghi789jkl012mno345pqr678stu901vwx234yz
```

## Application-Specific Setup

### Sonarr / Radarr

1. Go to `Settings → Download Clients`
2. Select `Show Advanced`
3. Add a new **qBittorrent** client
4. Set the host and port of qui
5. Add URL Base (`/proxy/...`) - remember to include `/qui/` if you use custom baseurl
6. Click **Test** and then **Save** once the test succeeds

### autobrr

1. Open `Settings → Download Clients`
2. Add **qBittorrent** (or edit an existing one)
3. Enter the full url like: `http://localhost:7476/proxy/abc123def456ghi789jkl012mno345pqr678stu901vwx234yz`
4. Leave username/password blank and press **Test**
5. Leave basic auth blank since qui handles that

For cross-seed integration with autobrr, see the [Cross-Seed](/docs/features/cross-seed/autobrr) section.

### cross-seed

1. Open cross-seed config file
2. Add or edit the `torrentClients` section
3. Append the full url following the documentation:
   ```
   torrentClients: ["qbittorrent:http://localhost:7476/proxy/abc123def456ghi789jkl012mno345pqr678stu901vwx234yz"],
   ```
4. Save the config file and restart cross-seed

### Upload Assistant

1. Open the Upload Assistant config file
2. Add or edit `qui_proxy_url` under the qBitTorrent client settings
3. Append the full url like: `"qui_proxy_url": "http://localhost:7476/proxy/abc123def456ghi789jkl012mno345pqr678stu901vwx234yz",`
4. All other auth type can remain unchanged
5. Save the config file

## Supported Applications

This reverse proxy will work with any application that supports qBittorrent's Web API.

## Security Features

- **API Key Authentication** - Each client requires a unique key
- **Instance Isolation** - Keys are tied to specific qBittorrent instances
- **Usage Tracking** - Monitor which clients are accessing your instances
- **Revocation** - Disable access instantly by deleting the API key
- **No Credential Exposure** - qBittorrent passwords never leave qui

## Intercepted Endpoints

The proxy intercepts certain qBittorrent API endpoints to improve performance and enable qui-specific features. Most requests are forwarded transparently to qBittorrent.

### Read Operations (Served from qui)

These endpoints are served directly from qui's sync manager for faster response times:

| Endpoint | Description |
|----------|-------------|
| `/api/v2/torrents/info` | Torrent list with standard qBittorrent filtering |
| `/api/v2/torrents/search` | Enhanced torrent list with fuzzy search (qui-specific) |
| `/api/v2/torrents/categories` | Category list from synchronized data |
| `/api/v2/torrents/tags` | Tag list from synchronized data |
| `/api/v2/torrents/properties` | Torrent properties |
| `/api/v2/torrents/trackers` | Torrent trackers with icon discovery |
| `/api/v2/torrents/files` | Torrent file list |

These endpoints proxy to qBittorrent and update qui's local state:

| Endpoint | Description |
|----------|-------------|
| `/api/v2/sync/maindata` | Full sync data (updates qui's cache) |
| `/api/v2/sync/torrentPeers` | Peer data (updates qui's peer state) |

### Write Operations

| Endpoint | Behavior |
|----------|----------|
| `/api/v2/auth/login` | No-op, returns success if instance is healthy |
| `/api/v2/torrents/reannounce` | Delegated to reannounce service when tracker monitoring is enabled |
| `/api/v2/torrents/setLocation` | Forwards to qBittorrent, invalidates file cache |
| `/api/v2/torrents/renameFile` | Forwards to qBittorrent, invalidates file cache |
| `/api/v2/torrents/renameFolder` | Forwards to qBittorrent, invalidates file cache |
| `/api/v2/torrents/delete` | Forwards to qBittorrent, invalidates file cache |

All other endpoints are forwarded transparently to qBittorrent.