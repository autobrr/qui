---
sidebar_position: 15
title: Dashboard
description: Instance statistics, live updates, and interface preferences that sync across devices.
---

# Dashboard

The Dashboard is the start page of qui. It shows statistics for every qBittorrent instance and combined totals across all instances.

## Sections

The Dashboard has four sections. You can hide and reorder each one. Server Statistics and Tracker Breakdown also collapse.

### Server Statistics

The section header shows the combined all-time download, upload, ratio, and peer count across all instances. Expand the section to see one row for each instance that reports all-time data:

| Column | Content |
|--------|---------|
| Downloaded / Uploaded | All-time totals that qBittorrent reports |
| Downloaded (Session) / Uploaded (Session) | Totals since qBittorrent last started |
| Ratio | All-time upload divided by all-time download |
| Peers | Current peer connections |

If no instance reports all-time data yet, qui hides this section.

### Tracker Breakdown

One row per tracker, aggregated across all instances:

| Column | Content |
|--------|---------|
| Tracker | Display name or announce domain, with the tracker icon |
| Uploaded / Downloaded | All-time totals for the torrents on this tracker |
| Uploaded (Session) / Downloaded (Session) | Totals for this qBittorrent session |
| Ratio | Upload divided by download |
| Buffer | Upload minus download |
| Torrents | Number of torrents |
| Size | Total content size |
| Seeded | Upload divided by content size |

Click a column header to sort. The row actions rename trackers or merge several announce domains into one entry. See [Tracker Customizations](./tracker-customizations.md).

### Global Stats Cards

Cards with combined numbers: connected instances, total torrents, total download speed, and total upload speed.

### Instance Cards

One card per instance with:

- Torrent counts: **Downloading**, **Active**, **Total**
- Current download and upload speed, total size, and free disk space
- qBittorrent, Web API, and libtorrent versions, and the qBittorrent connection status (connectable, firewalled, or disconnected). The tooltip shows the listen port.
- **Show more** reveals uptime, peer connections, queued I/O jobs, buffer sizes, and external IP addresses
- A turtle or rabbit button that toggles alternative speed limits after a confirmation dialog
- Warning rows for unregistered torrents, torrents with inactive trackers, and errors. Click a row to open the torrent list with that filter active.

The eye icon next to the instance URL toggles [Incognito Mode](./incognito.md) for the whole app. On the Dashboard it blurs host names and external IP addresses and replaces tracker names with fake domains.

## Configure the layout

Click **Layout Settings** to open the **Dashboard Settings** dialog.

| Setting | Description | Default |
|---------|-------------|---------|
| Sections | Show, hide, and reorder the four sections | All visible |
| Default Sort | Sort column for Tracker Breakdown | Uploaded |
| Direction | Sort direction for Tracker Breakdown | Descending |
| Items Per Page | Tracker Breakdown rows per page (10, 15, 25, or 50) | 15 |

qui stores these choices and the collapsed state of Server Statistics and Tracker Breakdown in its database. Every browser and device shows the same layout.

## Live updates

qui streams statistics changes to the browser over Server-Sent Events (SSE). The Dashboard applies stream updates every 2 seconds. If the stream is down, the Dashboard polls a lightweight endpoint every 5 seconds instead.

A status icon next to the instance name indicates that the numbers are not live:

| Icon | Meaning |
|------|---------|
| Red alert circle | The stream failed. qui refreshes over the polling fallback. |
| Database | qui shows a cached snapshot. If the snapshot is stale, the icon turns amber. |
| Circular arrows | qui refreshes the statistics over polling. |

A card without a status icon shows live data. If a qBittorrent instance responds slowly, qui serves the last cached data instead of an error until fresh data arrives.

## Interface preferences sync

qui stores interface preferences in its database, so they apply on every browser and device. This covers:

- Theme selection
- Torrent table columns: visibility, order, sorting, and widths
- Active filters, sidebar state, and collapsed categories
- View mode, speed units, language, incognito mode, and date and time format

Each browser keeps a local copy for fast startup. After a short delay, the browser pushes changes to the server automatically. Other open tabs receive these updates through the live stream. You do not need to configure anything.

:::note
qui is single-user software. Interface preferences and the Dashboard layout belong to the installation, not to a browser or an account. Everyone who logs in to the same qui sees the same layout.
:::
