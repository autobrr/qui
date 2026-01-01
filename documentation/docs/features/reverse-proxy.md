---
sidebar_position: 6
---

# Reverse Proxy for External Applications

qui includes a built-in reverse proxy that allows external applications like autobrr, Sonarr, Radarr, and other tools to connect to your qBittorrent instances **without needing qBittorrent credentials**. qui handles authentication transparently, making integration seamless.

## How It Works

The reverse proxy feature:
- **Handles authentication automatically** - qui manages the qBittorrent login using your configured credentials
- **Isolates clients** - Each client gets its own API key
- **Provides transparent access** - Clients see qui as if it were qBittorrent directly
- **Reduces login thrash** - qui maintains a shared cookie jar and session, so your automation tools stop racing to re-authenticate against qBittorrent. That means fewer failed logins, less load on qBittorrent, and faster announce races because downstream apps reuse the live session instead of waiting for new tokens.

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

## Troubleshooting

### Connection Refused Error

- Ensure qui is listening on all interfaces: `QUI__HOST=0.0.0.0 ./qui serve`
- Check that the port is accessible from your external application

### Authentication Errors

- Verify the Client API Key is correct and hasn't been deleted
- Ensure the key is mapped to the correct qBittorrent instance

### Version String Errors

- This was a common issue that's now resolved with the new proxy implementation
- Try regenerating the Client API Key if you still see version parsing errors
