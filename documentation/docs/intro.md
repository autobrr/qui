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
- **Themeable**: Multiple color themes available
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

## Browser Extensions

Right-click any magnet or torrent link to add it directly to your qBittorrent instances:

- [Chrome Extension](https://chromewebstore.google.com/detail/kbjnjgihepmcoilegnghgpmijbecoili)
- [Firefox Add-on](https://addons.mozilla.org/en-US/firefox/addon/qui/)

## Quick Start

Get started in minutes:

1. [Install qui](/docs/getting-started/installation)
2. Open your browser to http://localhost:7476
3. Create your admin account
4. Add your qBittorrent instance(s)
5. Start managing your torrents

## Community

Join our friendly and welcoming community on [Discord](https://discord.autobrr.com/qui)! Connect with fellow autobrr users, get advice, and share your experiences.

## License

GPL-2.0-or-later
