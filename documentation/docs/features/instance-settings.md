---
sidebar_position: 3
title: Add qBittorrent instances to qui
sidebar_label: Instance Settings
description: Connect one or more qBittorrent instances to qui, with connection settings, local filesystem access, incognito mode, and qBittorrent preferences.
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

After the **Instance** tab, the settings dialog shows the tabs of the qBittorrent WebUI in the same order: **Behavior**, **Downloads**, **Connect**, **Speed**, **BitTorrent**, **RSS**, **WebUI**, and **Advanced**. qui sends these preferences to the qBittorrent API. They work the same as in the qBittorrent WebUI.

qui shows a preference only when your qBittorrent version sends it and your libtorrent version and platform use it. qui hides a tab that has no preferences. The **WebUI** tab has no preferences in qui yet.

Some preferences are read-only. qui shows the value, and you change it in qBittorrent.

All tabs share one form. When you click **Save Changes** on any tab, qui sends only the preferences that you changed, on all tabs. If the save fails, qui shows an error and keeps your changes in the form.

Other screens in qui read the preferences each time that you open them. An example is the default save path in the add torrent dialog. If you change a preference in the qBittorrent WebUI, qui shows the new value the next time that you open the screen. qui can take up to 30 seconds to get the change from qBittorrent.

### Behavior

The **Log file** section sets whether qBittorrent writes its log to a file, and the folder of that file. It also has the backup of the log file at a maximum size in KiB, and the deletion of backups that are older than a number of days, months, or years. qBittorrent 4.6 and later send these preferences.

The other settings on the **Behavior** tab of the qBittorrent WebUI change only the qBittorrent WebUI or the desktop app. Examples are the language and the saved search tabs. qui does not show them.

### Downloads

The **When adding a torrent** section has the default content layout, **Add to top of queue**, **Start Torrents Paused**, and the torrent stop condition. The stop condition stops a new torrent when it receives its metadata or when qBittorrent checked its files. The section also sets when qBittorrent deletes the .torrent file that it added, for example from a watched folder. The choices are never, when the torrent is added, and always. "Always" also deletes the file when you cancel the add.

The **When adding a duplicate torrent** section sets whether qBittorrent adds the trackers of the duplicate to the existing torrent.

The **Backup of .torrent files** section sets the folders where qBittorrent keeps a copy of each .torrent file. On qBittorrent 5.3 and later, a switch turns on each folder. You can move the copy to a second folder when the torrent finishes, and remove the copy when you remove the torrent. Before qBittorrent 5.3, an empty folder turns the copy off.

The next section has the pre-allocation of disk space, the `.!qB` extension for incomplete files, and the `.unwanted` folder for files that you did not select. qBittorrent 5.0 and later send the `.unwanted` folder preference.

The **Saving management** section has automatic torrent management and its relocation rules, the category paths in Manual Mode, subcategories, the default save path, the temporary download path, and the watch folders. qBittorrent 4.6 to 5.1 send the subcategories preference.

The **Excluded file names** section has a list of file name patterns, one per line. qBittorrent does not download a file whose name matches a pattern. qBittorrent 4.5 and later send these preferences.

On qBittorrent 5.0 and later, a path field suggests folders on the qBittorrent host while you type. On qBittorrent 4.x, a path field is a plain text box.

The **Run External Program** section has the commands that qBittorrent runs when a torrent is added and when a torrent finishes. A list of placeholders follows them.

#### Start torrents paused

The **Start Torrents Paused** switch on the **Downloads** tab changes the qBittorrent preference "Do not start the download automatically". Torrents that other tools add to qBittorrent also start paused.

The add torrent dialog uses this preference as the default for its **Start paused** option. The choice in the dialog always applies to the torrents that you add.

:::note
Earlier versions of qui kept this setting in qui and did not send it to qBittorrent. After you update qui, open the **Downloads** tab and set **Start Torrents Paused** again.
:::

#### Monitored folders

qBittorrent watches folders and adds any torrent file that appears in them. Configure this in qui:

1. Open the instance settings dialog and select the **Downloads** tab.
2. Under **Watch Folders**, click **Add Folder**.
3. Enter the path of the folder to monitor.
4. Select the **Torrent Destination** for discovered torrents: **Monitored folder**, **Default save location**, or **Other** with a custom save path.
5. Click **Save Changes**.

### Connect

The **Protocol Settings** section has the BitTorrent protocol: TCP, μTP, or both.

The **Listening Port** section has the port for incoming connections and UPnP/NAT-PMP port forwarding.

The **Connection Limits** section has the global and per-torrent maximum connections and upload slots.

The **IP Filtering** section has the IP filter file, the filter for trackers, and the manually banned IP addresses.

### Speed

The **Global rate limits** and **Alternative rate limits** sections show the limits in KiB/s. An empty limit means no limit. The alternative rate limits section also has the schedule: the start time, the end time, and the days.

The **Rate limits settings** section sets whether the limits apply to μTP, to transport overhead, and to peers on the LAN.

### BitTorrent

The **Security & Privacy** section has DHT, PeX, local peer discovery, encryption, and anonymous mode.

The **Queue Management** section has queueing, the limits for active torrents, and the thresholds for slow torrents.

The **Seeding Limits** section has the share ratio limit, the seeding time limit, and the inactive seeding time limit. The times are in minutes. qBittorrent 4.6 and later send the inactive seeding time limit. The section also has the action at a limit. The action stops the torrent, removes it, removes it and its files, or enables super seeding. On qBittorrent 5.3 and later, you also select whether the action starts when the torrent reaches one limit or only when it reaches all limits.

The **Automatically add trackers to new downloads** section has the list of trackers that qBittorrent adds to each new torrent.

The **Automatically add trackers from a URL to new downloads** section has the URL of a tracker list. qBittorrent downloads the list from the URL and adds its trackers to each new torrent. The fetched trackers are read-only. qBittorrent 5.1 and later send these preferences.

### RSS

The **RSS reader** section has RSS processing, the refresh interval, the delay in seconds between two requests to the same host, and the maximum number of articles for each feed. qBittorrent 5.0 and later send the request delay.

The **RSS torrent auto downloader** section has RSS auto-downloading.

The **RSS smart episode filter** section sets whether qBittorrent downloads REPACK and PROPER episodes. It also has the episode filters. These are regular expressions, one per line, that find the season and the episode number in an article title.

### Advanced

The **qBittorrent** section has the memory working set limit, the network interface and its IP address, the lookup of peer countries, and the reannounce to trackers when the IP address changes. The network interface and its IP address are read-only. qui shows the memory working set limit only on libtorrent 2, and not on Linux or macOS.

The **libtorrent** section has these preferences:

- The asynchronous I/O threads, the hashing threads, the file pool size, the memory for torrent checks, the disk cache, and the disk queue size.
- The disk I/O type, the read mode, and the write mode, and the coalescing of reads and writes.
- The send buffer watermarks, the socket backlog size, the outgoing ports, and the UPnP lease duration.
- The peer ToS byte, the μTP-TCP mixed mode, multiple connections from one IP address, and the block of peers on privileged ports.
- The announce to all trackers and to all tiers, the IP address that qBittorrent reports to trackers, the maximum concurrent HTTP announces, and the stop tracker timeout.
- The peer turnover and the request queue size.

qui shows the disk cache and the coalescing of reads and writes only on libtorrent 1. qui shows the hashing threads and the disk I/O type only on libtorrent 2.
