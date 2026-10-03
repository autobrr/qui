---
sidebar_position: 1
title: Web UI for qBittorrent
sidebar_label: Introduction
description: Fast, modern interface for qBittorrent with cross-seeding, automations, and backups.
---

# qui

qui is a web interface for qBittorrent. It runs as one binary and connects to one or more qBittorrent instances over the Web API.

Most people run qui with one qBittorrent instance. Multi-instance support is optional. If you have a seedbox and a home server, qui manages both from one page.

## Where to run qui

qui can run on any machine that can reach the qBittorrent Web API. A few features read files on disk and need [local filesystem access](./features/instance-settings.md#local-filesystem-access). For those, qui must run on the same host as qBittorrent, or see the same files through a shared mount.

If you have one server, run qui on that server. Some seedbox providers install qui for you, see [Seedbox installers](./getting-started/seedbox-installers.md). If your provider gives you no shell, run qui at home and add the seedbox as a remote instance. A remote instance gets the interface, the automations that do not read the disk, and cross-seed in default mode.

## Features

- **Single Binary**: No dependencies. Download and run.
- **Multi-Instance Support**: Manage all your qBittorrent instances from one place
- **Large Collections**: Handles thousands of torrents
- **Themeable**: Multiple built-in color themes, plus sideloadable [custom themes](./features/custom-themes.md) (premium), with a shared collection in [qui-community-themes](https://github.com/autobrr/qui-community-themes)
- **Base URL Support**: Serve from a subdirectory (for example `/qui/`) behind a reverse proxy
- **OIDC Single Sign-On**: Authenticate through your [OpenID Connect provider](./configuration/oidc.md)
- **External Programs**: Launch [custom scripts](./features/external-programs.md) from the torrent context menu
- **Tracker Reannounce**: If qBittorrent does not retry fast enough, qui [reannounces stalled torrents](./features/reannounce.md)
- **Automations**: [Rule-based torrent management](./features/automations.md) with conditions, actions (delete, pause, tag, limit speeds), and cross-seed awareness
- **Orphan Scan**: [Find and remove files](./features/orphan-scan.md) that no torrent uses
- **Backups & Restore**: [Scheduled snapshots](./features/backups.md) with incremental, overwrite, and complete restore modes
- **Cross-Seed**: [Find and add matching torrents](./features/cross-seed/overview.md) across trackers, with autobrr webhook integration
- **Torrent Management**: [Tags, categories, saved filter views, torrent creation, and export](./features/torrent-management.md)
- **Dashboard**: [Per-instance statistics](./features/dashboard.md) with live updates over SSE
- **Search**: [Search your Torznab indexers](./features/search.md) and send results to an instance
- **RSS**: [qBittorrent RSS feeds and auto-download rules](./features/rss.md)
- **Magnet Links**: Register qui as your browser's handler for magnet links from **Settings → Security**
- **Reverse Proxy**: [Transparent qBittorrent proxy](./features/reverse-proxy.md) for external apps like autobrr, Sonarr, and Radarr, without credential sharing
- **Incognito Mode**: [Disguise torrents as Linux ISOs](./features/incognito.md) for screen sharing and screenshots
- **Multi-Language**: Interface available in English, German, French, Italian, Czech, Ukrainian, Korean, Brazilian Portuguese, Simplified Chinese, Traditional Chinese, and Catalan with automatic browser-language detection

## Browser extensions

Right-click a magnet or torrent link to add it to your qBittorrent instances:

- [Chrome Extension](https://chromewebstore.google.com/detail/kbjnjgihepmcoilegnghgpmijbecoili)
- [Firefox Add-on](https://addons.mozilla.org/en-US/firefox/addon/qui/)

To register qui as your browser handler for magnet links, open **Settings → Security** and click **Register as Handler**. The button shows only when you open qui over HTTPS or on localhost, in a browser that supports protocol handlers.

## Languages

qui is available in English, German, French, Italian, Czech, Ukrainian, Korean, Brazilian Portuguese, Simplified Chinese, Traditional Chinese, and Catalan. The interface detects your browser language on first load and remembers your choice after that.

Sizes and speeds follow the language: French shows `1,5 Gio` where English shows `1.5 GiB`. If a unit reads wrong in yours, say so in a Discussion.

To change the language, click the globe icon at the bottom of the sidebar. If the sidebar is collapsed, use the globe submenu in the top-right menu. On a phone, use the globe submenu under **Settings** in the footer bar.

Community members contribute translations. To add or improve a language, start a [Discussion](https://github.com/autobrr/qui/discussions/new/choose) or contact the team on [Discord](https://discord.autobrr.com/qui). The file [`web/AGENTS.md`](https://github.com/autobrr/qui/blob/develop/web/AGENTS.md) documents the translation workflow.

## Quick start

1. [Install qui](./getting-started/installation.md).
2. Open http://localhost:7476 in your browser.
3. Create your admin account.
4. Add your qBittorrent instances.

## Community

Join the community on [Discord](https://discord.autobrr.com/qui) to get help, share feedback, and meet other autobrr users.

## License

GPL-2.0-or-later

## Supported torrent clients

qui supports qBittorrent only. It communicates directly with the qBittorrent Web API. If you use Deluge, rTorrent, or Transmission, use the qui CLI to [migrate your torrents](./features/client-migration.md) to qBittorrent. The migration keeps their history.

For compatible qBittorrent versions, see the [qBittorrent Version Compatibility](./advanced/compatibility.md) page.
