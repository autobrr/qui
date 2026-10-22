---
sidebar_position: 2
title: Supported qBittorrent versions
sidebar_label: Compatibility
description: Supported qBittorrent versions (4.3.9 and newer) and the features that require a newer build.
---

# qBittorrent version compatibility

:::note
qui supports qBittorrent 4.3.9 and newer as the baseline. Some features require newer builds, as the table shows. Versions older than 4.3.9 can still connect, but qui does not guarantee full functionality.
:::

qui detects the features that each qBittorrent instance supports and adjusts the interface to match. If an instance is too old for a feature, qui disables that feature:

| Feature | Minimum Version | Notes |
| --- | --- | --- |
| **Rename Torrent** | 4.1.0+ (Web API 2.0.0+) | Change the display name of a torrent |
| **Tracker Editing** | 4.1.5+ (Web API 2.2.0+) | Edit, add, and remove tracker URLs |
| **File Priority Controls** | 4.1.5+ (Web API 2.2.0+) | Enable or disable files and set download priority levels |
| **Rename File** | 4.2.1+ (Web API 2.4.0+) | Rename individual files in a torrent |
| **Rename Folder** | 4.3.3+ (Web API 2.7.0+) | Rename folders in a torrent |
| **Per-Torrent Temporary Download Path** | 4.4.0+ (Web API 2.8.4+) | When you add a torrent, set a custom temporary download path |
| **Torrent Export (.torrent download)** | 4.5.0+ (Web API 2.8.11+) | Download .torrent files with `/api/v2/torrents/export`. First available in 4.5.0beta1 |
| **Backups (.torrent archive export)** | 4.5.0+ (Web API 2.8.11+) | qui backups use `/torrents/export`. If the endpoint is unavailable, qui hides the backup UI |
| **Subcategories** | 4.6.0+ (Web API 2.9.0+) | Nested category structures, for example `Movies/Action` |
| **Torrent Creation** | 5.0.0+ (Web API 2.11.2+) | Create new .torrent files with the Web API |
| **Path Autocomplete** | 5.0.0+ (Web API 2.11.2+) | When you add torrents or create .torrent files, qui suggests paths |
| **External IP Reporting (IPv4/IPv6)** | 5.1.0+ (Web API 2.11.3+) | Exposes the `last_external_address_v4` / `_v6` fields |
| **Tracker Health Status** | 5.1.0+ (Web API 2.11.4+) | Detects unregistered torrents and tracker issues |
| **Share limit action** | 5.2.0+ (Web API **2.15.1**+) | If a torrent reaches a ratio, seeding time, or inactive seeding limit, choose an action: stop, remove, remove with content, or enable super seeding. If the instance reports Web API **2.15.1** or newer, qui shows this control |
| **Share limit mode** | 5.3.0+ (Web API **2.16.0**+) | Controls whether that action runs when the torrent reaches **any** configured limit or only when it reaches **all** limits. If the instance reports Web API **2.16.0** or newer, qui shows this control |

:::note
Hybrid and v2 torrent creation requires a qBittorrent build that links against libtorrent v2. If a build links against libtorrent 1.x, it ignores the `format` parameter.
:::

## Category folders

A category with no save path saves its torrents to a folder with the name of the category. qBittorrent 5.3 changed how it makes the folder name. qBittorrent 5.2 and earlier replace each run of the characters `: ? " * < > |` with one space. qBittorrent 5.3 and later remove the spaces at the start and the end of the name, and replace each run of reserved characters with one `_`. The reserved characters depend on the platform that qBittorrent was built for:

- Linux: control characters only.
- macOS: control characters and `:`.
- Windows: control characters and `\ < > : " | ? *`. qBittorrent also removes dots and spaces at the end of the name, and adds `_1` to a reserved device name such as `con`.

For example, the category `tv:uhd` uses the folder `tv uhd` on qBittorrent 5.2, `tv:uhd` on qBittorrent 5.3 on Linux, and `tv_uhd` on qBittorrent 5.3 on macOS and Windows.

qui uses the rule of the qBittorrent version and the build platform of each instance. Thus the cleanup of empty folders and the orphan scan keep the correct category folder.

## Tracker health refresh

qui checks the tracker health of each torrent in the background every 60 seconds. Tracker health is the state that qui shows for the trackers of a torrent: unregistered, tracker down, or tracker error.

- On qBittorrent 5.2.0 and newer, the sync data of qBittorrent tells qui which torrents have a tracker problem. qui asks for the tracker details of a torrent only when its problem starts or changes. Thus, qui shows a new tracker problem within 60 seconds after qBittorrent reports it. If only the tracker message changes, qui shows the new message within 10 minutes. If a torrent has more than one tracker, a change of one tracker can also take up to 10 minutes to show. For example, a torrent with one working tracker and one tracker that is down stays healthy in qui for up to 10 minutes after the working tracker also goes down.
- On qBittorrent 5.1, qui asks for the tracker details of all torrents every 60 seconds. On a library with tens of thousands of torrents, this request can make qBittorrent slow for several seconds.
- On qBittorrent older than 5.1, qui shows no tracker health. The tracker sidebar counts a torrent under each of its trackers, also its backup trackers.

## Authentication compatibility

### API key auth with reverse-proxy Basic Auth

qBittorrent added API key authentication in version 5.2.0 (Web API 2.14.1). Older instances accept only a username and a password.

qBittorrent API key authentication uses the HTTP `Authorization: Bearer ...` header. Reverse-proxy Basic Auth, for example nginx `auth_basic`, also uses the `Authorization` header.

A request can carry only one normal `Authorization` value. You cannot combine qBittorrent API key authentication with reverse-proxy Basic Auth in the default setup. Use qBittorrent username and password authentication with reverse-proxy Basic Auth, or bypass Basic Auth for the requests that qui sends to qBittorrent.

## Troubleshooting missing features

### Create Torrent button is not visible

qui shows the **Create Torrent** button after it connects to qBittorrent **5.0.0** (Web API 2.11.2) or newer and detects torrent creation support. If the button is missing, open the [instance settings](../features/instance-settings.md), test the connection, and refresh the qui web UI. Upgrade qBittorrent if it is older than 5.0.0. On the all-instances view, qui shows the button for each instance where it detected support.

### Hybrid and v2 torrent formats are unavailable

The **hybrid** and **v2** torrent format options require a qBittorrent build that links against **libtorrent v2.x**, even on qBittorrent 5.0.0 or newer. If your build uses libtorrent 1.x, the torrent creation dialog displays an alert that only the **v1** format is available. This is a build-time property of qBittorrent. You cannot change it through qui.

### "Too many active torrent creation tasks" error

qBittorrent limits the number of concurrent torrent creation tasks. If you see a **409 Conflict** error with this message, wait for your existing creation tasks to finish before you start new ones. Monitor active tasks in the torrent creation task list.

### Download of a created torrent fails with "Failed to download torrent file"

The creation task is **Finished**, but the download shows this error. The qui log shows `status code: 409: unexpected status code`. The cause is the qBittorrent option **Delete .torrent files afterwards** (Options > Downloads).

With **Add to qBittorrent** enabled, qBittorrent writes the created file to disk and adds the torrent from that file. When **Delete .torrent files afterwards** is enabled, qBittorrent deletes the file after it adds the torrent. This applies to the temporary file that qBittorrent uses when **Save .torrent to** is empty, and to a file at a path that you set. The download fails because the file no longer exists. qBittorrent does not log this error.

Disable **Delete .torrent files afterwards** in qBittorrent, or disable **Add to qBittorrent** in the creator dialog and add the downloaded file yourself.
