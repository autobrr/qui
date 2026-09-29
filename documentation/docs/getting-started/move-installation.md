---
sidebar_position: 5
title: Move or migrate qui to a new server
sidebar_label: Move to a new server
description: Transfer a qui installation to another host. Copy qui.db and config.toml to keep your qBittorrent instances, users, automations, and cross-seed configuration.
---

# Move qui to a new server

qui keeps its state in two files. If you copy both files to the new host, qui starts there with the same instances, users, automations, and cross-seed configuration. You do not need a qBittorrent backup for this.

`qui.db` is the SQLite database that holds this data. If you use Postgres, qui has no `qui.db`, and your data stays in Postgres.

`config.toml` is the configuration file. It contains `sessionSecret`. qui makes the encryption key for the stored qBittorrent passwords from that value. If you start with a new `config.toml`, qui cannot read those passwords.

Both files are in the config directory:

| Install | Config directory |
| --- | --- |
| Linux or macOS | `~/.config/qui/` |
| Windows | `%APPDATA%\qui\` |
| Docker | The host folder that you mount at `/config` |

If you set `QUI__DATA_DIR`, `qui.db` is in that directory. If you start qui with `--config-dir`, use that directory.

## Copy the files

1. Stop qui on the old host. If qui runs, the copy of `qui.db` can be incomplete.
2. Copy `qui.db` and `config.toml` from the config directory on the old host.
3. Install qui on the new host. Use the [installation](./installation.md), [Docker](./docker.md), or [Windows](./windows.md) guide.
4. Stop qui on the new host.
5. In the config directory on the new host, delete `qui.db`, `qui.db-shm`, and `qui.db-wal` if they exist.
6. Put the two copied files in the config directory on the new host.
7. Start qui on the new host.

Log in with the user and password from the old host.

## After the move

- If the address of qBittorrent changed, edit each instance and set the new URL.
- If the paths to your data changed, update the paths in cross-seed, automations, and orphan scan.
- If `config.toml` on the new host needs a different host, port, or base URL, edit it before you start qui.
- To keep your [backups](../features/backups.md), also copy the `backups` directory. Copy the `tracker-icons` and `themes` directories if you want to keep them.

Do not run qui on both hosts with the same database. Both would then change your qBittorrent instances at the same time.
