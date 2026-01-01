---
sidebar_position: 2
---

# qBittorrent Version Compatibility

:::note
qui officially supports qBittorrent 4.3.9 and newer as the baseline. The features below may require newer builds as noted, and anything older than 4.3.9 might still connect, but functionality is not guaranteed.
:::

qui automatically detects the features available on each qBittorrent instance and adjusts the interface accordingly. Certain features require newer qBittorrent versions and will be disabled when connecting to older instances:

| Feature | Minimum Version | Notes |
| --- | --- | --- |
| **Rename Torrent** | 4.1.0+ (Web API 2.0.0+) | Change the display name of torrents |
| **Tracker Editing** | 4.1.5+ (Web API 2.2.0+) | Edit, add, and remove tracker URLs |
| **File Priority Controls** | 4.1.5+ (Web API 2.2.0+) | Enable/disable files and adjust download priority levels |
| **Rename File** | 4.2.1+ (Web API 2.4.0+) | Rename individual files within torrents |
| **Rename Folder** | 4.3.3+ (Web API 2.7.0+) | Rename folders within torrents |
| **Per-Torrent Temporary Download Path** | 4.4.0+ (Web API 2.8.4+) | A custom temporary download path may be set when adding torrents |
| **Torrent Export (.torrent download)** | 4.5.0+ (Web API 2.8.11+) | Download .torrent files via `/api/v2/torrents/export`; first appeared in 4.5.0beta1 |
| **Backups (.torrent archive export)** | 4.5.0+ (Web API 2.8.11+) | qui backups rely on `/torrents/export`; the backup UI is hidden when the endpoint is unavailable |
| **Subcategories** | 4.6.0+ (Web API 2.9.0+) | Support for nested category structures (e.g., `Movies/Action`) |
| **Torrent Creation** | 5.0.0+ (Web API 2.11.2+) | Create new .torrent files via the Web API |
| **Path Autocomplete** | 5.0.0+ (Web API 2.11.2+) | Autocomplete suggestions for path inputs when adding torrents or creating .torrent files |
| **External IP Reporting (IPv4/IPv6)** | 5.1.0+ (Web API 2.11.3+) | Exposes `last_external_address_v4` / `_v6` fields |
| **Tracker Health Status** | 5.1.0+ (Web API 2.11.4+) | Automatically detects unregistered torrents and tracker issues |

:::note
Hybrid and v2 torrent creation requires a qBittorrent build that links against libtorrent v2. Builds compiled with libtorrent 1.x ignore the `format` parameter.
:::
