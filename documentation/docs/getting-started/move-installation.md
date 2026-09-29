---
sidebar_position: 5
title: Move or migrate qui to a new server
sidebar_label: Move to a new server
description: Transfer a qui installation to another host. Copy qui.db and config.toml to keep your qBittorrent instances, users, automations, and cross-seed configuration.
---

# Move qui to a new server

qui keeps its state in `qui.db` and `config.toml`. If you copy these files to the new host, qui starts there with the same instances, users, automations, and cross-seed configuration. You do not need a qBittorrent backup for this.

`qui.db` is the SQLite database that holds this data. qui keeps it in the data directory.

`config.toml` is the configuration file. It contains `sessionSecret`. qui makes the encryption key for stored passwords and API keys from that value. If you start with a new `config.toml`, qui cannot read those passwords and API keys.

## Find the files

The config directory is the directory that contains the configuration file that qui uses. By default, it is:

| Install | Config directory |
| --- | --- |
| Linux or macOS | `~/.config/qui/` |
| Windows | `%APPDATA%\qui\` |
| Docker | The host folder that you mount at `/config` |

Two cases change this:

- If a `config.toml` is in the directory where you start qui, qui uses that file.
- If you start qui with `--config-dir`, qui uses the path that you give. The path can be a directory that contains `config.toml`, or a `.toml` file with a different name. In the second case, copy that file.

The data directory is the config directory, unless you set one of these:

- The `dataDir` value in `config.toml`
- The `QUI__DATA_DIR` environment variable
- The `--data-dir` flag

## Environment variables

Environment variables override the values in `config.toml`. If you set `QUI__SESSION_SECRET` or `QUI__SESSION_SECRET_FILE` on the old host, set the same value on the new host. If the secret changes, qui cannot read the stored passwords and API keys.

Copy the other `QUI__` environment variables that you set, for example `QUI__DATA_DIR`.

A variable that ends in `_FILE` names a file, for example `QUI__SESSION_SECRET_FILE` or `QUI__DATABASE_PASSWORD_FILE`. Copy each of these files to the new host. If a file has a new location, change the path in the variable.

## Postgres

If you use Postgres, qui has no `qui.db`. The data stays in the Postgres database. Copy only `config.toml`, and skip the steps for `qui.db` below.

Make sure that the new host can connect to the same Postgres database. This guide does not move the Postgres database itself.

## Copy the files

1. Stop qui on the old host. If qui runs, the copy of `qui.db` can be incomplete.
2. On the old host, copy `qui.db` from the data directory. If `qui.db-wal` and `qui.db-shm` exist, copy them too. They can hold recent changes that are not in `qui.db` yet.
3. On the old host, copy `config.toml` from the config directory.
4. Install qui on the new host. Use the [installation](./installation.md), [Docker](./docker.md), or [Windows](./windows.md) guide.
5. Stop qui on the new host.
6. In the data directory on the new host, delete `qui.db`, `qui.db-wal`, and `qui.db-shm` if they exist.
7. Put the copied `qui.db`, `qui.db-wal`, and `qui.db-shm` in the data directory on the new host.
8. Put `config.toml` in the config directory on the new host.
9. Start qui on the new host.

Log in with the user and password from the old host.

## After the move

- If the address of qBittorrent changed, edit each instance and set the new URL.
- If the paths to your data changed, update the paths in cross-seed, automations, and orphan scan.
- If the new host needs a different host, port, or base URL, edit `config.toml` before you start qui.
- If you want to keep your [backups](../features/backups.md), copy the `backups` directory too. Do the same for the `tracker-icons` and `themes` directories.

Do not run qui on both hosts with the same database. If you do, both change your qBittorrent instances at the same time.
