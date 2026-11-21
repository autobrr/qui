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

1. In the torrent list, select 1 to 100 torrents of one instance. If you select more than 100 torrents, the menu does not show **Share with someone…**.
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

The Share tracker lets your friend's client find your client. Open **Settings → Shares** and select one of two trackers:

- **qui's own tracker**: use it when you can open one port of the qui host to the internet. You need a port forward on your router, or a reverse proxy.
- **An outside tracker**: use it when you cannot open a port of the qui host. qui then sees no announces, so a Share gives you less control.

### qui's own tracker

qui's own tracker is off until you set a port and a public address. Read these facts before you set it up:

- The tracker has its own port. This port is not the port of the qui web interface, which is `7476` by default. The tracker listens on each network address of the qui host.
- qui opens the port only while a Share runs. A port test shows a closed port while no Share runs.
- Your own qBittorrent client announces to the public address too. Your friend's client gets your address only after that announce. Thus, your qBittorrent client must be able to open the public address.

To set up the tracker:

1. Select a port that no other program uses, for example `7477`.
2. If qui runs in Docker, publish the port. Add the port to `ports` of the qui service, and create the container again:

   ```yaml
   ports:
     - "7476:7476"
     - "7477:7477"
   ```

3. Let the internet reach the port. Do one of these:
   - Forward the TCP port on your router to the qui host. The public address is then `http://<your public IP address or host name>:7477`.
   - Put a reverse proxy in front of the port. The public address is then the URL of the proxy, for example `https://share.example.com`. See [Behind a reverse proxy](#behind-a-reverse-proxy).
4. Open **Settings → Shares** and select **qui's own tracker**.
5. Enter the port in **Port** and the URL in **Public address**.
6. If you use a reverse proxy, enter its address in **Trusted proxies**.
7. Click **Save**. If another program uses the port, qui shows `Another program uses port <port>. Pick another port.`
8. Make sure that other clients can connect to your qBittorrent client. See [The address of your client](#the-address-of-your-client).
9. Test the tracker. See [Test the tracker](#test-the-tracker).

| Field | Description |
| --- | --- |
| **Port** | The port on which the tracker listens. qui opens the port only while a Share runs. |
| **Public address** | The URL at which the internet reaches this port, for example `https://share.example.com`. |
| **Trusted proxies** | The addresses or CIDR ranges of your reverse proxies, one for each line. |

The `.torrent` of each Share contains the public address. Thus, a change of the public address waits until each Share on qui's tracker ends.

The tracker answers only for active Shares, and only to a client that has the `.torrent` of the Share. Any other request gets a `404`. The tracker is not an open tracker, and it does not show the login page of qui. It tells each client to announce every 5 minutes, and answers 30 announces each minute from one address. It gives IPv4 and IPv6 addresses.

#### Behind a reverse proxy

The tracker uses plain HTTP. For HTTPS, put a reverse proxy in front of the port. The proxy must send each request to the tracker port and keep the path, which starts with `/announce/`. If the public address has a path of its own, the proxy must remove that path.

This Caddy configuration sends `https://share.example.com` to a tracker on port `7477` of the same host. Caddy adds the `X-Forwarded-For` header.

```text
share.example.com {
	reverse_proxy 127.0.0.1:7477
}
```

In **Trusted proxies**, enter the address of each proxy between the internet and qui. Enter the address from which the reverse proxy connects to qui. If you use a CDN such as Cloudflare, also enter the published IP ranges of the CDN. Each proxy in a chain must pass on the `X-Forwarded-For` header that it receives and add its own entry.

If qui runs directly on the host or on the Docker host network, a proxy on the same host connects to qui from `127.0.0.1`. If qui runs in Docker on a bridge network, a proxy on the host connects from the gateway address of that bridge network. On the default bridge network, this address is `172.17.0.1`. A custom Docker network has its own gateway address. For a proxy in Docker, enter the address of the proxy container. You can enter a Docker network range only if no BitTorrent client is inside that range.

Do not enter your local network or any range that includes a BitTorrent client. A client whose address is inside a trusted range can get a `404`. The tracker reads `X-Forwarded-For` only when the connection comes from a trusted proxy. It reads the header from right to left and skips each trusted address until it finds an untrusted address. If it reaches an invalid entry before an untrusted address, it stops. If the tracker finds no untrusted address, or if `X-Forwarded-For` is missing, the tracker has no client address and answers with `404`.

Without a trusted proxy, the tracker sees each client at the address of the proxy. Then **Only the first downloader** gives the lock to the proxy, so each client that comes through the proxy gets your address. The limit of 30 announces each minute then counts all clients together.

#### The address of your client

The tracker gives your friend the public peer address of your client, and your friend's client connects to that address. If qBittorrent runs in Docker or behind NAT, set the [public peer address](./instance-settings.md#public-peer-address) of the instance. Also forward the listen port of qBittorrent on your router.

If qui does not know the address, it does not start the Share. It shows `qui does not know the public peer address of <instance>. Set it in the settings of the instance.`

#### Test the tracker

1. Start a Share of a small torrent. The tracker port is closed while no Share runs.
2. Open the public address in a browser on a network outside your own, for example on a phone with mobile data. An error page with the code `404` shows that the tracker answers. If the browser cannot connect, the port forward or the reverse proxy is wrong.
3. In qBittorrent, find the Share torrent by its tag `qui-share`, and open its **Trackers** tab. The status **Working** shows that your client can announce.
4. Send the `.torrent` to your friend. After your friend's client announces, the **Shares** tab shows its address.

If you test with a second client on your own network, make sure that its address is outside **Trusted proxies**.

### An outside tracker

Use an outside tracker when the internet cannot reach qui. The tracker must accept a torrent that it does not know. A public open tracker does, and so does a tracker that you run. A private tracker refuses the Share torrent, because the Share torrent has a new infohash.

:::warning
Do not enter the announce URL of a private tracker. qui puts the URL in the `.torrent` that you send to your friend, with each key or passkey that the URL contains.
:::

To set up an outside tracker:

1. Open **Settings → Shares** and select **An outside tracker**.
2. Enter an `http`, `https`, or `udp` announce URL in **Tracker URL**, for example `udp://tracker.example.org:1337/announce`.
3. Click **Save**. qui's own tracker stops listening.

Your client and your friend's client must both be able to reach the tracker. The Share torrent is private, so the clients find each other only through the tracker. The tracker gives your friend the address from which your client announced. qui does not use the public peer address of the instance here. Your friend's client connects to the listen port of qBittorrent, so forward that port on your router.

With an outside tracker, qui does not see the announces. Thus:

- The list of addresses on the Shares tab shows the peers that your client sees.
- qui cannot see when a download completes. For **End at the first complete download**, qui reads the bytes that your client sent and its list of peers every 30 seconds. The Share ends when your client sent as many bytes as the torrent has and no peer in the list still downloads. This is best effort. If two clients each download a part of the files, the Share can end before one of them has every file. If the Share does not end after the download, click **Stop**.
- **Only the first downloader** is not available.

The outside tracker sees the IP address of your client and of your friend's client.

### If the download does not start

A client announces again only every 5 minutes. After each fix, use **Force reannounce** in the client, or wait 5 minutes.

| What you see | Cause | What to do |
| --- | --- | --- |
| qui shows `Set a tracker in Settings → Shares` | No tracker is set. | Set up one of the two trackers. |
| qui shows `qui does not know the public peer address` | The instance has no public peer address, and qBittorrent reports no external address or uses a random listen port. | Set the [public peer address](./instance-settings.md#public-peer-address) of the instance. |
| Your friend's client shows a tracker error, such as a timeout or a refused connection | The public address does not reach the tracker port. | [Test the tracker](#test-the-tracker). |
| Your friend's client shows the tracker error `404` | The Share ended, or the `.torrent` is from another Share. | Start a new Share and send its `.torrent`. |
| A client shows the tracker error `404` or `Not Found`, and the qui log shows `share tracker: 404: no client address` | The address of the client is inside **Trusted proxies**, or the `X-Forwarded-For` header is missing or invalid. | In **Settings → Shares**, list only the addresses of your proxies in **Trusted proxies**, not client addresses. Include each proxy in the chain. Make sure that the proxies send valid IP addresses in `X-Forwarded-For`. |
| The tracker works in your friend's client, but the client finds no peer | Your own client did not announce. | Open the **Trackers** tab of the Share torrent in qBittorrent. If the status is not **Working**, your client cannot open the public address. |
| The tracker works, the client finds no peer, and **Only the first downloader** is on | Another address holds the lock, for example the old address of your friend. | Click **Unlock** on the Shares tab. |
| Your friend's client finds your client, but the download stays at 0% | Your friend's client cannot connect to your client. | Make sure that the public peer address is correct, and forward the listen port of qBittorrent. |
| Your friend's client shows `this Share has as many peers as it takes` | The Share has its number of recipient clients. | Increase **Peers for each Share**. |

Some routers do not let a device in your network open the public address of the router. Then your own client cannot announce to qui's tracker. Let the host name of the public address point to the reverse proxy inside your network, or use an outside tracker.

### Limits

| Field | Default | Description |
| --- | --- | --- |
| **Active Shares** | 10 | The number of Shares that can run at the same time. |
| **Peers for each Share** | 20 | The number of recipient clients that the tracker accepts for one Share. Your own client does not count. |

A Share has no upload speed limit. To limit the upload, use the speed limits of qBittorrent.
