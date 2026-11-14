---
sidebar_position: 15
title: qBittorrent statistics dashboard in qui
sidebar_label: Dashboard
description: "The start page of qui: the upload and download of the last 24 hours, the live speed, each qBittorrent instance, and the items that need an action."
---

# Dashboard

The Dashboard is the start page of qui. It shows how much data your qBittorrent instances move now, how much they moved in the last 24 hours, and what needs an action.

The page has a chart at the top, a list of the instances below it, and the largest trackers of the last 24 hours. On a wide screen, a column at the right shows **Needs attention**, **Storage**, and **Library**. On a phone, these parts are in one column.

## Chart

The chart shows the upload above the zero line and the download below it, for all instances together. It counts the instances as the Statistics page counts **All instances**, so it also counts a disabled or removed instance that moved data in the last 24 hours. Each of the first five active instances in the instance order has its own layer, in a shade of the text color of the theme. The other instances share one layer.

The switch above the chart has two views:

| View | Content |
|------|---------|
| **24 h** | The bytes that qui recorded in each hour of the last 24 hours. The data comes from the [transfer history](./statistics.md). |
| **Live** | The speed of the last 3 minutes. qui keeps these samples in the browser while the page is open. The chart starts empty when you open the page, and again when you come back to a hidden tab. |

The page opens on **24 h**. If no active instance recorded any transfer in the last 24 hours, the page opens on **Live**, and **24 h** is off. Its tooltip gives the reason. If qui cannot read the history, the chart shows the error in place of the numbers.

The upload speed and the download speed of this moment are the two large numbers above and below the chart. Below each number is a caption:

- In **24 h**, the upload caption gives the upload of the last 24 hours and the share from [Statistics cross-seeds](./statistics.md). The download caption gives the download of the last 24 hours. These numbers equal the **Uploaded** and **Downloaded** numbers of the Statistics page for the period **24 hours** and all instances.
- In **Live**, the upload caption gives the count of seeding torrents and the count of peers. The download caption gives the count of downloading torrents and the size that is left to download.

A dashed yellow line marks an hour that includes a recording gap. A recording gap is a time when qui did not read the instance, for example while qui did not run. If the recording started less than 24 hours ago, the chart has the label **Partial period**. The page reads the history again each 5 minutes, because qui writes the history each 5 minutes.

The Dashboard shows no all-time counter of qBittorrent. The [Statistics](./statistics.md) page shows the recorded totals.

## Instances

Each instance has one line. The line shows the name, the upload speed, the download speed, and the count of torrents with a problem. On a wide screen, the list is a table that also shows the torrent count and the upload of the last 24 hours, with a bar for its share.

An offline instance shows the cause in place of the numbers. A status icon next to the name tells you that the numbers are not live. See [Live updates](#live-updates).

On a phone, tap a line to open its details in a sheet at the bottom of the screen. On a wide screen, click a row to open its details below the row. One row is open at a time. The details show these values:

- The upload speed and the download speed, and the upload and the download of the last 24 hours. On a wide screen, a chart shows the last 24 hours of the instance.
- Each problem count as a link. A click opens the torrent list of the instance with the filter for that problem.
- The host, the torrent counts (total, active, downloading, and seeding), the size, and the free space at the default save path of qBittorrent. The size is the [Unique size](#unique-size).
- The peer connections, the uptime, the qBittorrent version, the connection status, and the external IP addresses.

The details have three actions: **Open torrents**, **Add torrent**, and the switch for the alternative speed limits. The switch asks for a confirmation before it changes the limits.

If you tap or click an offline instance, qui shows the cause and a link to the instance settings.

## Top trackers

This part shows the five trackers with the most upload in the last 24 hours. Each row has the series color of the Statistics page, the name, the upload, and the share of the upload. The other trackers are one gray row **Other**. The rows add up to the upload of the last 24 hours. The [series colors](./statistics.md#series-colors) setting of the Statistics page applies to this list.

The **Statistics** link opens the Statistics page with the period **24 hours**. To rename or merge trackers, see [Tracker Customizations](./tracker-customizations.md).

If no active instance recorded any transfer in the last 24 hours, this part is not on the page.

## Needs attention

This list shows each item that needs an action:

| Item | Color |
|------|-------|
| An instance that is offline | Red |
| An instance with a disk that is almost full | Red |
| An instance that is firewalled | Yellow |
| A count of torrents that are unregistered, have a tracker that is down, have a tracker error, or have an error | The color of the problem |

A disk is almost full when the free space is less than 5% of the free space plus the Unique size of the instance. A System notice is not in this list. qui shows a System notice in the sidebar, the header, and the mobile footer.

## Storage and Library

**Storage** shows the free disk space of each instance at the default save path of qBittorrent.

**Library** shows the torrent count, the total size, the seeding size, and the count of connected instances. The total size and the seeding size are the [Unique size](#unique-size).

## Incognito mode

The eye button in the page header turns [Incognito Mode](./incognito.md) on and off for the whole app. On the Dashboard, incognito mode hides the hosts, the external IP addresses, and the cause of an offline instance, because the error text can hold the host or its IP address. It replaces the tracker names with fake domains.

### Unique size

A cross-seed often uses the data of another torrent. In hardlink or reflink mode, the two torrents have different folders, but the disk holds the data one time. The Unique size counts the copies of a release one time. Two torrents are copies when qui puts them in one group of [Group copies](./torrent-management.md#group-copies). You do not have to turn on the **Group copies** switch. Torrents with the same content path also count one time.

A torrent can hold only some of the files of a release. For example, it holds two of the three episodes of a season pack, it is a REPACK that changes one file, or it is a single episode of a season pack that you also have. Such a torrent is not in the group, but the Unique size counts the files that it shares with the release one time. When two torrents of one release each have a file of the same size, qui counts the two files as one file.

qui does not make sure that two copies share their data on the disk. If you downloaded a release two times, or an episode and then its season pack, the disk holds two copies, but the Unique size counts the data one time.

qui counts the copies one time in each instance. A copy on a second instance counts again.

qui finds the copies in the background from its start. The first time, it reads the file lists of the torrents that can have a copy, four torrents at a time. qui keeps the lists and reads a list again only when its torrent changes. Until the first search after a start of qui ends, the Dashboard shows the old size with the mark **calculating**. The old size counts torrents with the same content path one time, but it counts a hardlinked or reflinked cross-seed in full.

The sidebar of the torrent list does not use the Unique size for categories and tags.

## Live updates

qui streams statistics changes to the browser over Server-Sent Events (SSE). The Dashboard applies stream updates every 2 seconds. If the stream connection is down, the Dashboard polls a lightweight endpoint every 5 seconds instead. If the stream sends no data for 2 minutes, the Dashboard polls every 30 seconds until data comes again. If qBittorrent returns an error, the Dashboard does not poll. The open stream sends the new numbers when qBittorrent answers again.

A status icon next to the name of an instance tells you that its numbers are not live:

| Icon | Meaning |
|------|---------|
| Red alert circle | The stream failed. If the connection to qui dropped, qui refreshes over polling. If qBittorrent returned an error, qui waits for the stream. |
| Database | qui shows a cached snapshot. If the snapshot is stale, the icon turns amber. |
| Circular arrows | qui refreshes the statistics over polling. |

An instance without a status icon shows live data. If a qBittorrent instance responds slowly, qui serves the last cached data instead of an error until fresh data arrives.

## Interface preferences sync

qui stores interface preferences in its database, so they apply on every browser and device. This covers:

- Theme selection
- Torrent table columns: visibility, order, sorting, and widths
- Active filters, sidebar state, and collapsed categories
- View mode, speed units, language, incognito mode, and date and time format

Each browser keeps a local copy for fast startup. After a short delay, the browser pushes changes to the server automatically. Other open tabs receive these updates through the live stream. You do not need to configure anything.

:::note
qui is single-user software. Interface preferences belong to the installation, not to a browser or an account. Everyone who logs in to the same qui sees the same preferences.
:::
