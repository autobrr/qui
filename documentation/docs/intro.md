---
sidebar_position: 1
slug: /
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

1. [Install qui](/getting-started/installation)
2. Open your browser to http://localhost:7476
3. Create your admin account
4. Add your qBittorrent instance(s)
5. Start managing your torrents

## Community

Join our friendly and welcoming community on [Discord](https://discord.autobrr.com/qui)! Connect with fellow autobrr users, get advice, and share your experiences.

## Support Development

qui is developed and maintained by volunteers. Your support helps us continue improving the project.

### License Key

Pay what you want (minimum $4.99) to unlock premium themes:
- Use any payment method below (GitHub Sponsors, Buy Me a Coffee, or crypto)
- After paying, DM soup/ze0s on Discord (depending on who you paid to)
  - For crypto, include the transaction hash/link
- You'll receive a 100% discount code
- Redeem the code on [Polar](https://buy.polar.sh/polar_cl_yyXJesVM9pFVfAPIplspbfCukgVgXzXjXIc2N0I8WcL) (free order) to receive your license key
- Enter the license key in Settings → Themes in your qui instance
- License is lifetime

### Support Methods

- **soup**
  - [GitHub Sponsors](https://github.com/s0up4200)
  - [Buy Me a Coffee](https://buymeacoffee.com/s0up4200)
- **zze0s**
  - [GitHub Sponsors](https://github.com/zze0s)
  - [Buy Me a Coffee](https://buymeacoffee.com/ze0s)

#### Cryptocurrency

To get a qui license with crypto, send the transaction link to soup or ze0s on Discord.

| Currency | soup | zze0s |
|----------|------|-------|
| BTC | `bc1qfe093kmhvsa436v4ksz0udfcggg3vtnm2tjgem` | `bc1q2nvdd83hrzelqn4vyjm8tvjwmsuuxsdlg4ws7x` |
| ETH | `0xD8f517c395a68FEa8d19832398d4dA7b45cbc38F` | `0xBF7d749574aabF17fC35b27232892d3F0ff4D423` |
| LTC | `ltc1q86nx64mu2j22psj378amm58ghvy4c9dw80z88h` | `ltc1qza9ffjr5y43uk8nj9ndjx9hkj0ph3rhur6wudn` |
| XMR | `8AMPTPgjmLG9armLBvRA8NMZqPWuNT4US3kQoZrxDDVSU21kpYpFr1UCWmmtcBKGsvDCFA3KTphGXExWb3aHEu67JkcjAvC` | `44AvbWXzFN3bnv2oj92AmEaR26PQf5Ys4W155zw3frvEJf2s4g325bk4tRBgH7umSVMhk88vkU3gw9cDvuCSHgpRPsuWVJp` |

All methods unlock premium themes — use whichever works best for you. For other currencies or payment methods, [reach out on Discord](https://discord.autobrr.com/qui).

## License

GPL-2.0-or-later
