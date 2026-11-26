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

The chart shows the upload above the zero line and the download below it, for all instances together. If the chart has no download, the upload uses the whole height of the chart. The chart counts the instances as the Statistics page counts **All instances**, so it also counts a disabled or removed instance that moved data in the last 24 hours. Each of the first five active instances in the instance order has its own layer, in a shade of the text color of the theme. The other instances share one layer.

The instance list is a table of rows on a wide screen and a list of lines on a phone. When the pointer is on an instance row, the chart draws its layer in the primary color of the theme, and the other layers fade. The same happens while a row has the keyboard focus. When the pointer is on a chart layer, the chart draws that layer in the primary color, and the other layers fade. The instance list also marks the instance row for that layer, or the instance line on a phone. On a touch screen, a tap on a layer does the same as the pointer.

For the layer that instances share, the instance list marks each of its instances, also when the pointer is on only one of them. If an instance has no data in the chart, all layers fade, and the chart does not draw any layer in the primary color. The chart and the instance list work this way in both chart views, **24 h** and **Live**.

The switch above the chart has two views:

| View | Content |
|------|---------|
| **24 h** | The bytes that qui recorded in each hour of the last 24 hours. The data comes from the [transfer history](./statistics.md). |
| **Live** | The speed of the last 3 minutes. The qui server keeps these samples for 3 minutes. It takes a sample each time that it reads an instance, and it sends no extra request to qBittorrent. While the Dashboard is open in a browser, qui reads each instance about every 2 seconds. Thus, if you open the page in a new tab or on another device, the chart shows the last 3 minutes at once. If no browser had the Dashboard open, qui reads each instance about once a minute. Then the chart starts empty and fills from the right. If qui did not read an instance for more than 6 seconds, that time shows as an empty stretch. A restart of qui clears the samples. |

The page opens on the view that you selected last. qui keeps this choice on the server, so it applies in each browser and on each device. If no active instance recorded any transfer in the last 24 hours, the page opens on **Live**, and **24 h** is off. qui keeps your choice for the time when history is available again. Its tooltip gives the reason. If qui cannot read the history, the chart shows the error in place of the numbers.

The upload speed and the download speed of this moment are the two large numbers above the chart. Below each number is a caption:

- In **24 h**, the upload caption gives the upload of the last 24 hours and the share from [Statistics cross-seeds](./statistics.md). The download caption gives the download of the last 24 hours. These numbers equal the **Uploaded** and **Downloaded** numbers of the Statistics page for the period **24 hours** and all instances.
- In **Live**, the upload caption gives the count of seeding torrents and the count of peers. The download caption gives the count of downloading torrents and the size that is left to download.

A dashed yellow line marks an hour that includes a recording gap. A recording gap is a time when qui did not read the instance, for example while qui did not run. If the recording started less than 24 hours ago, the chart has the label **Partial period**. The page reads the history again each 5 minutes, because qui writes the history each 5 minutes.

## Instances

Each instance has one line. The line shows the name, the upload speed, the download speed, and the count of torrents with a problem. On a wide screen, the list is a table that also shows the torrent count and the upload of the last 24 hours, with a bar for its share.

An offline instance shows the cause in place of the numbers. A status icon next to the name tells you that the numbers are not live. See [Live updates](#live-updates).

On a wide screen, the name of an online instance is a link to its torrent list. The button next to the name turns the alternative speed limits of the instance on or off with one click. The button shows a turtle. A gray turtle shows that the limits are off. A red turtle shows that the limits are on. qui does not ask for confirmation. An instance that is not online has no link and no button.

### Details of an instance

On a phone, tap a line to open its details in a sheet at the bottom of the screen. On a wide screen, click a row to open its details below the row. Click the row again to close the details.

On a wide screen, two or more rows can be open at the same time. The button in the header of the table opens each row (**Expand all**) or closes each row (**Collapse all**). qui keeps the open rows in the [Display profile](#display-profiles) of the browser, so each screen can have its own open rows. Each row is closed until you open it. A phone shows one sheet at a time, and qui does not keep which sheet is open.

The details show these values:

- The upload speed and the download speed.
- **Last 24 h**: the upload, the download, and the ratio of the last 24 hours. On a wide screen, a chart shows the last 24 hours of the instance.
- **All time**: the upload, the download, and the ratio that qBittorrent counted from its first start.
- Each problem count as a link. A click opens the torrent list of the instance with the filter for that problem.
- The host, the torrent counts (total, active, downloading, and seeding), the size, and the free space at the default save path of qBittorrent. The size is the [Unique size](#unique-size).
- The peer connections, the uptime, the qBittorrent version, the libtorrent version, the connection status, and the external IP addresses.

On a phone, the sheet has three actions: **Open torrents**, **Add torrent**, and the switch for the alternative speed limits. The switch changes the limits at once, and qui does not ask for confirmation.

If you tap or click an offline instance, qui shows the cause and a link to the instance settings.

## Top trackers

This part shows the five trackers with the most upload in the last 24 hours. Each row has the series color of the Statistics page, the [tracker icon](./tracker-icons.md), the name, the upload, and the share of the upload. The other trackers are one gray row **Other**. Public, No tracker, and Other have no icon. The rows add up to the upload of the last 24 hours. The [series colors](./statistics.md#series-colors) setting of the Statistics page applies to this list.

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

If no item needs an action, the page does not show the list.

A disk is almost full when the free space is less than 5% of the size of the disk. qui shows this item only when it can read the disk through Local Filesystem Access or SSH access. When qui estimates the **Storage** bar, qui never marks the disk as almost full. See [Storage and Library](#storage-and-library) for how qui reads the size of the disk. A System notice is not in this list. qui shows a System notice in the sidebar and the mobile footer.

## Storage and Library

**Storage** shows a row for each instance with its name, the free space at the default save path of qBittorrent, and a bar. qBittorrent reports the free space in each row, not the size of the disk.

When an instance has [Local Filesystem Access](./instance-settings.md#local-filesystem-access) or [SSH access](./ssh-access.md), qui reads the size and the free space of the disk at that path. The filled part of the bar shows the real used share of the disk. Two instances on one disk get the same bar. With SSH access, the server must report free disk space.

Without that access, or when qui cannot read the disk, qui estimates the bar. The filled part is the [Unique size](#unique-size) of the instance out of that size plus the free space. When at least one bar is an estimate, qui shows an info icon next to the **Storage** heading. Its text explains the estimate and names **Local Filesystem Access** and SSH access. When no bar is an estimate, qui does not show the icon.

**Library** shows the torrent count, the total size, the seeding size, and the count of connected instances. The total size and the seeding size are the [Unique size](#unique-size).

Below these numbers, **All time** shows the upload, the download, and the ratio that qBittorrent counted from its first start. These numbers are for all instances together. They come from qBittorrent, so they also include the transfer from before qui started to record. The [Statistics](./statistics.md) page shows the totals that qui recorded.

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

qui stores interface preferences in its database, so they follow you to each browser and device. Each browser keeps a local copy for fast startup. After a short delay, the browser sends each change to the server. Other open tabs get the change through the live stream. You do not need to configure anything.

qui puts each preference into one of three groups:

- Display settings follow the [Display profile](#display-profiles) of the browser.
- Sort and filters are the same in every profile, unless a profile [keeps its own](#own-sort-and-filters).
- All other preferences are the same in every browser. Examples are the language, the speed units, the date and time format, the delete defaults, incognito mode, and the view of the Dashboard chart.

The theme has its own sync. See [Theme selection syncs across devices](./custom-themes.md#theme-selection-syncs-across-devices). qui does not sync the height of the details panel. Each browser keeps its own height.

:::note
qui is single-user software. Interface preferences and Display profiles belong to the installation, not to a browser or an account. Everyone who logs in to the same qui sees the same profiles. The pick of a profile belongs to the browser.
:::

### Display profiles

A Display profile is a named set of Display settings. Display settings are the preferences that can be different on each screen:

- The [table columns](./torrent-management.md#table-columns): which columns show, their order, their widths, and the stretch or scroll mode of the Name column
- The row density of the torrent table, and the view of the mobile torrent list
- The app sidebar: full, icon-only, or hidden
- The filter sidebar, open or closed
- The open sections of the filter sidebar
- The collapsed category trees
- The switches that show empty statuses, categories, and tags
- The open rows of the instance table of the Dashboard

Each browser uses one profile, and only that browser remembers its pick. A browser that never picked a profile uses Default. A private window starts on Default. If you used qui before Display profiles, Default holds the Display settings that you had then.

A change to a Display setting goes to the profile of the browser. The other profiles do not change. If two browsers use the same profile, a change in one browser also shows in the other. If you clear the site data of a browser, the browser uses Default again. Pick your profile again to get its Display settings back.

To make a profile, click the profile chip in the status bar under the torrent table. Then click **New profile from this setup…**, enter a name, and click **Create**. The new profile copies these values from the profile of the browser:

- Every Display setting, for every instance
- The switches **Same columns on every instance** and **Own sort and filters**
- The own sort and filters, if the source profile keeps its own

The new profile does not copy the icon or the color. After **Create**, this browser uses the new profile. **Settings → Display profiles** has the same button.

A name has 1 to 32 characters. Two profiles cannot have the same name, also if the letter case is different. While you type the name, qui selects an icon that fits it, for example a smartphone for "Phone". To use a different icon or a color, click the icon button in the dialog.

To use a different profile in this browser, click the chip, and then click the profile. The table changes at once. On a phone, open the sort menu above the cards and tap **Display profile**. Then tap the profile in the sheet that opens. The sheet also has the **New profile from this setup…** button.

**Settings → Display profiles** lists the profiles. Click a profile to open it in the editor. In the editor, you can change the name, the icon, the color, and the two switches, and you can delete the profile. **Use in this browser** makes this browser use the profile. On a phone, tap a profile to use it, and tap **›** to open the editor. You can rename Default, but you cannot delete it. When you delete a profile, each browser that uses it moves to Default. The other browsers show a message, for example "The Laptop profile was deleted. This browser now uses Default."

### Own sort and filters

Sort and filters are these preferences:

- The sort of the torrent table and of the mobile torrent list
- The column filters and the sidebar filters
- The instances that the Unified view shows
- [Group copies](./torrent-management.md#group-copies), on or off
- The last open tab of the details panel
- The instance that the Backups and RSS pages show

By default, every profile shares them. Thus, a filter that you set on one screen also shows on a screen with a different profile. To give a profile its own sort and filters, open the profile in **Settings → Display profiles** and turn on **Own sort and filters**. The profile starts with a copy of the shared values, so nothing changes at that moment. After that, a change in this profile does not reach the other profiles, and a change in the other profiles does not reach this one. Default always shares its sort and filters, so it has no switch.

If you turn the switch off, qui deletes the own sort and filters of the profile. The profile then uses the shared values again. qui does not ask for a confirmation. The help line under the switch tells you this before you turn it off.
