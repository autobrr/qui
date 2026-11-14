---
sidebar_position: 15
title: qBittorrent statistics dashboard in qui
sidebar_label: Dashboard
description: "The start page of qui: per-instance and combined qBittorrent statistics, live updates, and interface preferences that sync across devices."
---

# Dashboard

The Dashboard is the start page of qui. It shows statistics for every qBittorrent instance and combined totals across all instances.

## Sections

The Dashboard has three sections. You can hide and reorder each one. Server Statistics also collapses.

The Dashboard has no table for each tracker. The [Statistics](./statistics.md) page shows the upload, download, ratio, buffer, torrents, size, and seeded value of each tracker for a period that you select. To rename or merge trackers, see [Tracker Customizations](./tracker-customizations.md).

### Server Statistics

The section header shows the combined all-time download, upload, ratio, and peer count across all instances. Expand the section to see one row for each instance that reports all-time data:

| Column | Content |
|--------|---------|
| Downloaded / Uploaded | All-time totals that qBittorrent reports |
| Downloaded (Session) / Uploaded (Session) | Totals since qBittorrent last started |
| Ratio | All-time upload divided by all-time download |
| Peers | Current peer connections |

If no instance reports all-time data yet, qui hides this section.

### Global Stats Cards

Cards with combined numbers: connected instances, total torrents, total download speed, and total upload speed. The torrent card also shows the total size, and the upload card shows the size of the torrents that seed now. Both sizes are the [Unique size](#unique-size).

### Instance Cards

One card per instance with:

- Torrent counts: **Downloading**, **Active**, **Total**
- Current download and upload speed, total size, and free space at the default save path of qBittorrent. The total size is the [Unique size](#unique-size). The card and the status bar of the torrent list show this value for the default path, not the space on other disks.
- qBittorrent, Web API, and libtorrent versions, and the qBittorrent connection status (connectable, firewalled, or disconnected). The tooltip shows the listen port.
- **Show more** reveals uptime, peer connections, queued I/O jobs, buffer sizes, and external IP addresses
- A turtle or rabbit button that toggles alternative speed limits after a confirmation dialog
- Warning rows for unregistered torrents, torrents whose tracker is down, torrents with a tracker error, and errors. A row shows only when its count is above zero. Click a row to open the torrent list with that filter active.

The eye icon next to the instance URL toggles [Incognito Mode](./incognito.md) for the whole app. On the Dashboard it blurs host names and external IP addresses and replaces tracker names with fake domains.

### Unique size

A cross-seed often uses the data of another torrent. In hardlink or reflink mode, the two torrents have different folders, but the disk holds the data one time. The Unique size counts the copies of a release one time. Two torrents are copies when qui puts them in one group of [Group copies](./torrent-management.md#group-copies). You do not have to turn on the **Group copies** switch. Torrents with the same content path also count one time.

A torrent can hold only some of the files of a release. For example, it holds two of the three episodes of a season pack, it is a REPACK that changes one file, or it is a single episode of a season pack that you also have. Such a torrent is not in the group, but the Unique size counts the files that it shares with the release one time. When two torrents of one release each have a file of the same size, qui counts the two files as one file.

qui does not make sure that two copies share their data on the disk. If you downloaded a release two times, or an episode and then its season pack, the disk holds two copies, but the Unique size counts the data one time.

qui counts the copies one time in each instance. A copy on a second instance counts again.

qui finds the copies in the background from its start. The first time, it reads the file lists of the torrents that can have a copy, four torrents at a time. qui keeps the lists and reads a list again only when its torrent changes. Until the first search after a start of qui ends, the Dashboard shows the old size with the mark **calculating**. The old size counts torrents with the same content path one time, but it counts a hardlinked or reflinked cross-seed in full.

The sidebar of the torrent list does not use the Unique size for categories and tags.

## Configure the layout

Click **Layout Settings** to open the **Dashboard Settings** dialog.

| Setting | Description | Default |
|---------|-------------|---------|
| Sections | Show, hide, and reorder the three sections | All visible |

qui stores these choices and the collapsed state of Server Statistics in its database. Every browser and device shows the same layout.

## Live updates

qui streams statistics changes to the browser over Server-Sent Events (SSE). The Dashboard applies stream updates every 2 seconds. If the stream connection is down, the Dashboard polls a lightweight endpoint every 5 seconds instead. If the stream sends no data for 2 minutes, the Dashboard polls every 30 seconds until data comes again. If qBittorrent returns an error, the Dashboard does not poll. The open stream sends the new numbers when qBittorrent answers again.

A status icon next to the instance name indicates that the numbers are not live:

| Icon | Meaning |
|------|---------|
| Red alert circle | The stream failed. If the connection to qui dropped, qui refreshes over polling. If qBittorrent returned an error, qui waits for the stream. |
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
