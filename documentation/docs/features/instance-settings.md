---
sidebar_position: 3
title: Instance Settings
description: Configure qBittorrent instance connections in qui.
---

# Instance Settings

Add and configure qBittorrent instances that qui connects to. Each instance represents a separate qBittorrent WebUI that qui can manage.

## Adding an Instance

1. Open qui and go to **Settings → Instances**
2. Click **Add Instance**
3. Enter connection details and click **Save**

## Instance Configuration

On the Dashboard, click the gear icon next to an instance name. In **Settings → Instances**, click the three-dot menu and select **Edit**.

### Connection Settings

| Field | Description |
|-------|-------------|
| **Name** | Display name shown in qui's sidebar and instance selector. |
| **Host** | Full URL to qBittorrent WebUI (e.g., `http://localhost:8080`). |
| **Skip TLS Verification** | Bypass certificate validation for self-signed certificates. |
| **Local Filesystem Access** | Enable for features requiring direct file access. |

### Authentication

qui supports multiple authentication methods depending on your setup:

| Option | When to Use |
|--------|-------------|
| **qBittorrent Login** | Enable and enter credentials for standard WebUI authentication. Disable if qBittorrent bypasses auth for localhost or whitelisted IPs. |
| **HTTP Basic Auth** | Enable when a reverse proxy adds Basic Authentication in front of qBittorrent. |

:::note
HTTP Basic Auth is separate from qBittorrent's built-in auth. Enable it when your reverse proxy (nginx, Caddy, etc.) requires credentials before reaching qBittorrent.
:::

## Local Filesystem Access

When enabled, qui can access the same filesystem as qBittorrent. This unlocks several features:

- **Content File Download** - Download individual files from a torrent's content directly through the browser (right-click a file in the Content tab).
- **Hardlink Detection** - Automations can detect whether torrent files have hardlinks to your media library.
- **Orphan Scan** - Find files on disk that aren't tracked by any torrent.
- **Free Space (Path)** - Automation rules can check free space on specific mount points instead of relying on qBittorrent's reported value.

:::warning
Only enable this if qui runs on the same machine (or has the same mounts) as qBittorrent. If paths don't match, features will fail silently or produce incorrect results.
:::

For Docker deployments, ensure the container has the necessary volume mounts. See [Docker configuration](/docs/getting-started/docker) for details.

## Instance Actions

At the bottom of the settings panel:

- **Enable / Disable** - Toggle whether qui actively connects to and manages this instance.
- **Delete** - Remove the instance from qui. This does not affect qBittorrent itself.

## qBittorrent Preferences

The settings dialog includes tabs for configuring qBittorrent's application preferences (speed limits, queue management, connection settings, etc.). These are passed directly to qBittorrent's API and behave identically to the native WebUI settings.
