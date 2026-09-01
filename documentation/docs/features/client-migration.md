---
sidebar_position: 13
title: Client Migration
description: Import torrents with their state from Deluge, rTorrent or Transmission into qBittorrent.
---

# Client migration

The `qui migrate` command imports torrents from Deluge, rTorrent, or Transmission into qBittorrent. It preserves their state: save paths, trackers, transfer totals, timestamps, seeding time, labels, paused state, and per-file selection. Completed torrents arrive in a verified state, so qBittorrent skips the recheck.

```bash
qui migrate {deluge | rtorrent | transmission} \
  --source-dir <client state dir> \
  --qbit-dir ~/.local/share/qBittorrent/BT_backup
```

Run with `--dry-run` first to preview the import. The dry run writes nothing.

## Before you start

1. **Stop the source client cleanly.** All three clients flush their resume state on shutdown. If you terminate the process abruptly, it leaves stale or missing state files.
2. **Stop qBittorrent.** The importer writes into qBittorrent's `BT_backup` directory. qBittorrent reads that directory only at startup.
3. After the migration finishes, start qBittorrent. The imported torrents appear with their history intact.

If you do not set `--skip-backup`, the command archives both directories to `qbt_backup/` in the current working directory before it writes anything. If the qBittorrent directory already exists, the command archives it. A fresh destination produces only the source archive. If you run the migration again, the command safely skips torrents that already exist in the target.

`--qbit-dir` is qBittorrent's session directory. Common locations: `~/.local/share/qBittorrent/BT_backup` on Linux, `%LOCALAPPDATA%\qBittorrent\BT_backup` on Windows, and `/config/qBittorrent/BT_backup` in Docker images.

:::warning
The command carries over save paths from the source client without changes. qBittorrent must see the downloaded data at those same paths. If you move between machines or containers, keep the mount layout identical. Otherwise, the torrents show as missing files. Run the migration on the same OS family as the source client. You cannot import a Unix session directory on a Windows host.
:::

## Deluge

Point `--source-dir` at the `state` directory inside the Deluge configuration directory:

```bash
qui migrate deluge \
  --source-dir ~/.config/deluge/state \
  --qbit-dir ~/.local/share/qBittorrent/BT_backup
```

Supported: Deluge 1.3.x and 2.x. The importer reads `torrents.fastresume` and the per-torrent `.torrent` files. If the importer cannot find or read `torrents.fastresume`, it falls back to the `.bak` copy and the pre-1.3 location.

- Labels from the Label plugin become the qBittorrent **category**. The importer reads these labels from `label.conf` in the configuration directory.
- The importer preserves Deluge 2.x paused state and file renames. Because Deluge 1.3.x marks the whole library paused on shutdown, the importer cannot preserve paused state for version 1.3.x. All Deluge 1.3.x imports start resumed.
- The importer skips BitTorrent v2 torrents. Their merkle trees do not survive this migration path, and qBittorrent rejects them.

## rTorrent

Point `--source-dir` at the session directory from your `.rtorrent.rc` (`session.path.set`):

```bash
qui migrate rtorrent \
  --source-dir ~/.sessions \
  --qbit-dir ~/.local/share/qBittorrent/BT_backup
```

Supported: rTorrent 0.9.x through 0.16.x, with or without ruTorrent.

- ruTorrent labels (`custom1`) become the qBittorrent **category**. If ruTorrent `addtime`/`seedingtime` timestamps exist, the importer uses them. Otherwise, it falls back to plain rTorrent timestamps.
- Both directory layouts work: the standard layout where the torrent folder sits inside the download directory, and `d.directory_base` layouts where files live directly in the download directory.
- Trackers keep their tiers from the torrent file. The importer excludes trackers that you disabled in rTorrent and includes trackers that you added at runtime.
- Stopped torrents stay stopped. The importer skips unfinished magnet downloads.
- rTorrent tracks no cumulative seeding counter. The importer calculates seeding time as elapsed time since seeding began, and counts offline time in that total. If you import a long-lived library, review your share limits before you run the migration.

## Transmission

Point `--source-dir` at the Transmission configuration directory (the one that contains `torrents/` and `resume/`):

```bash
qui migrate transmission \
  --source-dir ~/.config/transmission-daemon \
  --qbit-dir ~/.local/share/qBittorrent/BT_backup
```

Supported: Transmission 2.4 through 4.x. The importer supports legacy name-based session file names from 2.x. Resume files written by versions older than 2.4 have no block progress data, so the importer skips them. The log reports these torrents as not fully downloaded.

- Transmission labels become qBittorrent **tags**.
- Paused torrents stay paused. Per-torrent ratio and speed limits carry over.
- If you set files to "do not download", they keep priority 0 in qBittorrent.

## What is imported

| | Deluge | rTorrent | Transmission |
|---|---|---|---|
| Save path & content layout | ✓ | ✓ | ✓ |
| Verified piece state (no recheck) | ✓ | ✓ | ✓ |
| Upload/download totals & ratio | ✓ | ✓ | ✓ |
| Added / completed timestamps | ✓ | ✓ | ✓ |
| Seeding time | ✓ | ✓ | ✓ |
| Trackers | ✓ | ✓ | ✓ |
| Labels | category | category (ruTorrent) | tags |
| Paused state | 2.x only | ✓ | ✓ |
| Deselected files | ✓ | ✓ | ✓ |
| File renames | ✓ | — | — |
| Per-torrent ratio/speed limits | — | — | ✓ |

The importer tags every imported torrent `migrated` so you can find them with one filter. Running torrents import as auto-managed, so qBittorrent applies its queueing and share limits. Torrents that you stopped stay stopped and stay out of auto-management.

## Partial torrents

The importer imports only fully downloaded torrents. "Fully downloaded" means every file that you selected. If a torrent is still in progress, the importer skips it with a warning and leaves it in the source client. This check ensures that no torrent imports with incorrect piece state. If you have incomplete torrents, finish or remove them before you run the migration again.

:::note
On first start after a migration, qBittorrent logs a one-time warning per label that the category/tag was "missing from the configuration file" and recovers it. This warning is cosmetic.
:::

See [CLI Commands](../configuration/cli-commands.md#migrate-from-other-torrent-clients) for the full flag reference.
