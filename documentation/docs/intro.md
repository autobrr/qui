---
sidebar_position: 1
title: Introduction
description: Fast, modern interface for qBittorrent with cross-seeding, automations, and backups.
---

# qui

A web interface for qBittorrent. Manage multiple qBittorrent instances from a single application.

## Features

- **Single Binary**: No dependencies, just download and run
- **Multi-Instance Support**: Manage all your qBittorrent instances from one place
- **Large Collections**: Handles thousands of torrents efficiently
- **Themeable**: Multiple built-in color themes, plus sideloadable [custom themes](./features/custom-themes.md) (premium), with a shared collection in [qui-community-themes](https://github.com/autobrr/qui-community-themes)
- **Base URL Support**: Serve from a subdirectory (e.g., `/qui/`) for reverse proxy setups
- **OIDC Single Sign-On**: Authenticate through your OpenID Connect provider
- **External Programs**: Launch custom scripts from the torrent context menu
- **Tracker Reannounce**: Automatically fix stalled torrents when qBittorrent doesn't retry fast enough
- **Automations**: Rule-based torrent management with conditions, actions (delete, pause, tag, limit speeds), and cross-seed awareness
- **Orphan Scan**: Find and remove files not associated with any torrent
- **Backups & Restore**: Scheduled snapshots with incremental, overwrite, and complete restore modes
- **Cross-Seed**: Automatically find and add matching torrents across trackers with autobrr webhook integration
- **Reverse Proxy**: Transparent qBittorrent proxy for external apps like autobrr, Sonarr, and Radarr—no credential sharing needed
- **Incognito Mode**: Disguise torrents as Linux ISOs for screen sharing and screenshots
- **Multi-Language**: Interface available in English, German, French, Italian, Czech, Ukrainian, Korean, Brazilian Portuguese, Simplified Chinese and Traditional Chinese with automatic browser-language detection

## Browser Extensions

Right-click any magnet or torrent link to add it directly to your qBittorrent instances:

- [Chrome Extension](https://chromewebstore.google.com/detail/kbjnjgihepmcoilegnghgpmijbecoili)
- [Firefox Add-on](https://addons.mozilla.org/en-US/firefox/addon/qui/)

## Languages

qui is available in English, German, French, Italian, Czech, Ukrainian, Korean, Brazilian Portuguese, Simplified Chinese, and Traditional Chinese. The interface detects your browser language on first load and remembers your choice after that.

To change it manually, open the user menu in the top-right corner and pick a language from the globe submenu.

Translations are community-contributed and we welcome more. To add or improve a language, start a [Discussion](https://github.com/autobrr/qui/discussions/new/choose) or reach out on [Discord](https://discord.autobrr.com/qui). The translation workflow is documented in [`web/AGENTS.md`](https://github.com/autobrr/qui/blob/develop/web/AGENTS.md).

## Quick Start

Get started in minutes:

1. [Install qui](./getting-started/installation.md)
2. Open your browser to http://localhost:7476
3. Create your admin account
4. Add your qBittorrent instance(s)
5. Start managing your torrents

## Community

Join our friendly and welcoming community on [Discord](https://discord.autobrr.com/qui)! Connect with fellow autobrr users, get advice, and share your experiences.

## License

GPL-2.0-or-later

## Supported Torrent Clients

qui currently only supports qBittorrent. It communicates directly with the qBittorrent Web API. Support for other torrent clients such as Deluge, rTorrent, and Transmission is not yet available, but we hope to support them all in the future.

For details on which qBittorrent versions are compatible, see the [qBittorrent Version Compatibility](./advanced/compatibility.md) page.
