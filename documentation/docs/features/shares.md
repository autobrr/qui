---
sidebar_position: 15.7
title: Share the files of a torrent with someone outside qui
sidebar_label: Shares
description: "A Share gives the files of one torrent to someone whose BitTorrent client qui does not manage. qui makes a private copy of the torrent without the passkey, seeds it from your client, and lets the two clients find each other through its own tracker or an outside tracker. Read what a Share reveals before you start one."
---

# Shares

A Share gives the files of one torrent to a person whose BitTorrent client qui does not manage, for example a friend. qui makes a Share torrent and gives you its `.torrent` file and a short message. You send both to your friend in your own chat. Your friend opens the `.torrent` in a BitTorrent client and downloads the files from your client.

A Share torrent is a private copy of the torrent, with a new infohash and the same pieces. qui does not hash the files again, so a Share of a large torrent starts in one second. The Share torrent has no passkey, comment, web seed, "created by" line, or source tag of the real torrent. The trackers of the real torrent do not see it.

To move a torrent to another instance in qui, use a [Transfer](./transfers.md).

## What a Share reveals

Read this before you start a Share. While a Share runs, each person who has its `.torrent` can:

- See the IP address of your qBittorrent client and the port on which it listens.
- See the address and the port of the Share tracker.
- Download the files of the torrent from your client.

The `.torrent` is not a secret after you send it. Each person who gets a copy of it from your friend can also download, until the Share ends. **Only the first downloader** and **End at the first complete download** limit this. See [Start a Share](#start-a-share).

qui does not put the `.torrent` on a public link. Only a signed-in browser can start, stop, or download a Share. An [API key](../api/permissions.md#browser-only-routes) cannot, also when it holds every permission.

If you turned off authentication for listed networks with `QUI__AUTH_DISABLED` and `QUI__AUTH_DISABLED_ALLOWED_CIDRS`, each person on those networks can start a Share and download its `.torrent`. See [Environment variables](../configuration/environment.md).

## Requirements

- Share needs qBittorrent 5.1 or newer on the instance of the torrent. qui refuses an older instance with the message `Share needs qBittorrent 5.1 or newer. Update qBittorrent on <instance>.`
- A Share tracker. Set it in **Settings → Shares** before the first Share. See [Set up the Share tracker](#set-up-the-share-tracker).
- Your friend's client must be able to connect to your client. qui gives your friend's client the [public peer address](./instance-settings.md#public-peer-address) of the instance.

## Start a Share

1. In the torrent list, select one or more torrents of one instance.
2. Right-click and click **Share with someone…**. Each torrent gets its own Share.
3. Set the three fields. They start with the defaults from **Settings → Shares**:
   - **Days until a Share ends**: 7 by default, 365 at most.
   - **End at the first complete download**: the Share ends when one client has every file. On by default.
   - **Only the first downloader**: the tracker gives your address to the first client that downloads, and to no other client. On by default. This controls who learns your address from the tracker. It does not control who can connect to your client.
4. Click **Create**.
5. Click **.torrent** to download the file, and click **Copy message** to copy the message.
6. Send the `.torrent` and the message to your friend.

The dialog shows the Share tracker that new Shares use, with a link to **Settings → Shares**.

qui adds the Share torrent to your client with "Skip hash check", and seeds it from the files that your client already has. The torrent list shows a badge on the Share torrent. qui adds it with the tag `qui-share`. See [Helper torrents](./transfers.md#helper-torrents).

## When a Share ends

A Share ends when one of these occurs:

- One client completes the download, if **End at the first complete download** is on.
- The days of the Share pass.
- You click **Stop**.
- The real torrent is deleted, moved, or renamed. When you do this in qui, the Share ends first, and then qui does the change.
- The Share torrent leaves your client.

When a Share ends, qui removes the Share torrent from your client, and the open connections stop. qui never deletes files when a Share ends. If your client is offline, the Share shows **Ending** until qui can remove the Share torrent.

Shares continue after a restart of qui.

## The Shares tab

To see your Shares, open the **Transfers** page and click the **Shares** tab. Each Share shows its state, its end, the download progress, the bytes that your client sent, and the addresses that announced to the tracker. With an outside tracker, the tab shows the connected peers of your client instead.

Each Share has these actions:

- **.torrent** downloads the `.torrent` again.
- **Unlock** lets the next downloader in, for example after the address of your friend changed. The button shows only when **Only the first downloader** is on.
- **Stop** ends the Share.
- **Remove** removes an ended Share from the list.

Ended Shares stay in the list for 30 days.

## Set up the Share tracker

The Share tracker lets your friend's client find your client. Open **Settings → Shares** and select one of two trackers.

### qui's own tracker

qui's own tracker is off until you set a port and a public address. Use it when the internet can reach qui.

| Field | Description |
| --- | --- |
| **Port** | The port on which the tracker listens. qui opens the port only while a Share runs. |
| **Public address** | The URL at which the internet reaches this port, for example `https://share.example.com`. |
| **Trusted proxies** | The addresses or CIDR ranges of your reverse proxies, one for each line. |

The tracker uses plain HTTP. For HTTPS, put a reverse proxy in front of the port. The tracker reads the client address from `X-Forwarded-For` only when the request comes from a trusted proxy.

The `.torrent` of each Share contains the public address. Thus, a change of the public address waits until each Share on qui's tracker ends.

The tracker answers only for active Shares, and only to a client that has the `.torrent` of the Share. Any other request gets a `404`. The tracker is not an open tracker, and it does not show the login page of qui. It tells each client to announce every 5 minutes, and answers 30 announces each minute from one address. It gives IPv4 and IPv6 addresses.

The tracker gives your friend the public peer address of your client. If qBittorrent runs in Docker or behind NAT, set the [public peer address](./instance-settings.md#public-peer-address) of the instance.

### An outside tracker

Use an outside tracker when the internet cannot reach qui. Enter an `http`, `https`, or `udp` announce URL in **Tracker URL**. When you save the outside tracker, qui's own tracker stops listening.

With an outside tracker, qui does not see the announces. Thus:

- The list of addresses on the Shares tab shows the peers that your client sees.
- The end at the first complete download reads the peers of your client and the bytes that it sent. This is best effort.
- **Only the first downloader** is not available.

The outside tracker sees the IP address of your client and of your friend's client.

### Limits

| Field | Default | Description |
| --- | --- | --- |
| **Active Shares** | 10 | The number of Shares that can run at the same time. |
| **Peers for each Share** | 20 | The number of recipient clients that the tracker accepts for one Share. Your own client does not count. |

A Share has no upload speed limit. To limit the upload, use the speed limits of qBittorrent.
