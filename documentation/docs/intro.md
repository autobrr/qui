---
sidebar_position: 1
title: Introduction
---

# qui

A fast, modern web interface for qBittorrent. Supports managing multiple qBittorrent instances from a single, lightweight application.

## Features

- **Single Binary**: No dependencies, just download and run
- **Multi-Instance Support**: Manage all your qBittorrent instances from one place
- **Fast & Responsive**: Optimized for performance with large torrent collections
- **Clean Interface**: Modern UI built with React and shadcn/ui components
- **Multiple Themes**: Choose from various color themes
- **Base URL Support**: Serve from a subdirectory (e.g., `/qui/`) for reverse proxy setups
- **OIDC Single Sign-On**: Authenticate through your OpenID Connect provider
- **External Programs**: Launch custom scripts from the torrent context menu
- **Tracker Reannounce**: Automatically fix stalled torrents when qBittorrent doesn't retry fast enough
- **Automations**: Rule-based torrent management with conditions, actions (delete, pause, tag, limit speeds), and cross-seed awareness
- **Orphan Scan**: Find and remove files not associated with any torrent
- **Backups & Restore**: Scheduled snapshots with incremental, overwrite, and complete restore modes
- **Cross-Seed**: Automatically find and add matching torrents across trackers with autobrr webhook integration
- **Reverse Proxy**: Transparent qBittorrent proxy for external apps like autobrr, Sonarr, and Radarr—no credential sharing needed

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
