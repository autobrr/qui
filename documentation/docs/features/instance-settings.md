---
sidebar_position: 3
title: Instance Settings
description: Configure qBittorrent instance connections in qui.
---

# Instance Settings

Add and configure the qBittorrent instances that qui connects to. Each instance is a separate qBittorrent WebUI that qui manages.

## Add an instance

1. Open **Settings → Instances**.
2. Click **Add Instance**.
3. Enter the connection details and click **Add Instance**.

## Edit an instance

Two paths open the instance settings dialog:

- On the Dashboard, click the gear icon next to the instance name.
- In **Settings → Instances**, open the three-dot menu on the instance card and select **Edit**.

### Connection settings

| Field | Description |
|-------|-------------|
| **Instance Name** | Display name in qui's sidebar and instance selector. |
| **URL** | Full URL to the qBittorrent WebUI (for example, `http://localhost:8080`). |
| **Skip TLS Verification** (**Skip TLS Certificate Verification** in the Add Instance form) | Accept self-signed or otherwise untrusted certificates. |
| **Local Filesystem Access** | Enable for features that read files directly. See [Local Filesystem Access](#local-filesystem-access). |

### Authentication

Select how qui authenticates to qBittorrent under **qBittorrent Authentication**:

| Option | When to use |
|--------|-------------|
| **None** | qBittorrent bypasses authentication for localhost or whitelisted IPs. |
| **Username and Password** | Standard WebUI credentials. |
| **API Key** | The instance accepts an API key instead of credentials. |

If a reverse proxy requires credentials in front of qBittorrent, enable **HTTP Basic Authentication**.

:::note
HTTP Basic Authentication is separate from qBittorrent's built-in authentication. If your reverse proxy, for example nginx or Caddy, requires credentials before requests reach qBittorrent, enable it.
:::

## Local Filesystem Access

If enabled, qui reads the same filesystem as qBittorrent. This setting turns on these features:

- **Content file download**: Download single files from a torrent through the browser. Right-click a file in the Content tab.
- **Hardlink detection**: Automations detect whether torrent files have hardlinks into your media library.
- **Orphan scan**: Find files on disk that no torrent references.
- **Free space (path)**: Automation rules check free space on a specific mount point instead of the value that qBittorrent reports.
- **Has Missing Files condition**: Automation rules check whether a completed torrent has files missing on disk.
- **MediaInfo**: Show MediaInfo for a file in the Content tab.
- **Cross-seed hardlink and reflink mode**: Create links instead of a second copy.
- **Cross-seed ID fallback**: Read IMDb, TMDb, and TVDb tags from MKV files when a search finds no results.
- **Directory scan**: Use the instance as a directory scan target.

:::warning
Enable this setting only if qui runs on the same machine as qBittorrent or has the same mounts. If the paths differ, these features fail without an error or return wrong results.
:::

If qui runs in Docker, mount the same data volumes in the qui container. See [Docker configuration](../getting-started/docker.md) for details.

## Incognito mode

In **Settings → Instances**, click the eye icon next to an instance URL to toggle [incognito mode](./incognito.md).

## Instance actions

In the instance settings dialog, open the three-dot menu next to the dialog title:

- **Disable Instance** / **Enable Instance**: Control whether qui connects to and manages this instance.
- **Delete Instance**: Remove the instance from qui. This does not change qBittorrent itself.

## qBittorrent Preferences

The settings dialog has tabs for qBittorrent application preferences: **Speed**, **Queue**, **Files**, **Seeding**, **Connect**, **Discovery**, and **Advanced**. qui sends these values to qBittorrent's API. They behave the same as the native WebUI settings.

### Monitored folders

qBittorrent watches folders and adds any torrent file that appears in them. Configure this in qui:

1. Open the instance settings dialog and select the **Files** tab.
2. Under **Watch Folders**, click **Add Folder**.
3. Enter the path of the folder to monitor.
4. Select the **Torrent Destination** for discovered torrents: **Monitored folder**, **Default save location**, or **Other** with a custom save path.
5. Click **Save Changes**.
