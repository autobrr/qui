---
sidebar_position: 10
title: Client Migration
description: Import torrents with their state from Deluge, rTorrent or Transmission into qBittorrent.
---

# Client Migration

The `qui migrate` command imports torrents from Deluge, rTorrent or Transmission into qBittorrent, keeping their state: save paths, trackers, transfer totals, timestamps, seeding time, labels, paused state and per-file selection. Torrents arrive verified, so qBittorrent starts seeding them without a recheck.

```bash
qui migrate {deluge | rtorrent | transmission} \
  --source-dir <client state dir> \
  --qbit-dir ~/.local/share/qBittorrent/BT_backup
```

Run with `--dry-run` first to see what would be imported without writing anything.

## Before You Start

1. **Stop the source client cleanly.** All three clients flush their resume state on shutdown; a killed process can leave stale or missing state files.
2. **Stop qBittorrent.** The importer writes directly into qBittorrent's `BT_backup` directory, which qBittorrent only reads at startup.
3. Start qBittorrent after the migration finishes. The imported torrents appear immediately with their history intact.

Unless `--skip-backup` is set, both directories are archived to `qbt_backup/` in the current working directory before anything is written — the qBittorrent directory only when it already exists, so a fresh destination produces just the source archive. Re-running a migration is safe: torrents that already exist in the target are skipped.

The `--qbit-dir` is qBittorrent's session directory, commonly `~/.local/share/qBittorrent/BT_backup` on Linux, `%LOCALAPPDATA%\qBittorrent\BT_backup` on Windows, or `/config/qBittorrent/BT_backup` in Docker images.

:::warning
The save paths recorded by the source client are carried over as-is. qBittorrent must see the downloaded data at those same paths — when moving between machines or containers, keep the mount layout identical or the torrents will show as missing files. For the same reason, run the migration on the same OS family as the source client: a Unix session dir cannot be imported on a Windows host.
:::

## Deluge

Point `--source-dir` at the `state` directory inside the Deluge config dir:

```bash
qui migrate deluge \
  --source-dir ~/.config/deluge/state \
  --qbit-dir ~/.local/share/qBittorrent/BT_backup
```

Supported: Deluge 1.3.x and 2.x. The importer reads `torrents.fastresume` (falling back to the `.bak` copy and the pre-1.3 location) plus the per-torrent `.torrent` files.

- Labels from the Label plugin become the qBittorrent **category**, read from `label.conf` in the config dir.
- Deluge 2.x paused/resumed state and file renames are preserved. Deluge 1.3.x cannot preserve paused state — its resume data marks the whole library paused on shutdown — so all 1.3.x imports start resumed.
- BitTorrent v2 torrents are skipped: their merkle trees do not survive this migration path and qBittorrent would reject them.

## rTorrent

Point `--source-dir` at the session directory from your `.rtorrent.rc` (`session.path.set`):

```bash
qui migrate rtorrent \
  --source-dir ~/.sessions \
  --qbit-dir ~/.local/share/qBittorrent/BT_backup
```

Supported: rTorrent 0.9.x through 0.16.x, with or without ruTorrent.

- ruTorrent labels (`custom1`) become the qBittorrent **category**; ruTorrent's `addtime`/`seedingtime` timestamps are used when present, with sane fallbacks for plain rTorrent.
- Both directory layouts work: the standard one where the torrent's folder sits inside the download directory, and `d.directory_base` layouts where files live directly in it.
- Trackers keep their tiers from the torrent file; trackers you disabled in rTorrent stay out, trackers you added at runtime come along.
- Stopped torrents stay stopped. Unfinished magnet downloads are skipped.
- rTorrent keeps no cumulative seeding counter, so seeding time is approximated as time since seeding began — including time the client was offline. Check your share limits before importing a long-lived library.

## Transmission

Point `--source-dir` at the Transmission config directory (the one containing `torrents/` and `resume/`):

```bash
qui migrate transmission \
  --source-dir ~/.config/transmission-daemon \
  --qbit-dir ~/.local/share/qBittorrent/BT_backup
```

Supported: Transmission 2.4 through 4.x, including the legacy name-based session file naming from 2.x. Resume files last written by versions older than 2.4 use legacy progress and limit formats and are skipped.

- Transmission labels become qBittorrent **tags**.
- Paused torrents stay paused; per-torrent ratio and speed limits carry over.
- Files marked "do not download" keep priority 0 in qBittorrent.

## What Is Imported

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

Every imported torrent is tagged `migrated` so you can find them in one filter. Torrents import auto-managed, so qBittorrent's queueing and share limits apply; torrents you had stopped stay stopped.

## Partial Torrents

Only fully downloaded torrents are imported — "fully downloaded" meaning every file you actually selected. Torrents still mid-download are skipped with a warning and stay in the source client, so nothing is ever imported with incorrect piece state. Finish or remove them, then re-run the migration.

:::note
On first start after a migration, qBittorrent logs a one-time warning per label that the category/tag was "missing from the configuration file" and recovers it automatically. This is cosmetic.
:::

See [CLI Commands](../configuration/cli-commands.md#migrate-from-other-torrent-clients) for the full flag reference.
