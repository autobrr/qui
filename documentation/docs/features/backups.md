---
sidebar_position: 1
title: Backups
description: Schedule and restore qBittorrent instance backups.
---

# Backups and restore

qui creates scheduled and manual snapshots of a qBittorrent instance. Each snapshot includes the torrent archive, tags, categories with their save paths, and cached `.torrent` blobs. You can restore the original state from a snapshot at any time.

If you manage multiple instances, the Backups page provides **Save changes to all instances**. This action copies the current backup schedule and settings to every compatible instance in one step.

## Backup storage

qui writes backup snapshots to `<dataDir>/backups` by default. If you want to store snapshots in a different location, set `backupDir` in `config.toml` or set the `QUI__BACKUP_DIR` environment variable. A backup on the same drive as the live database does not protect against drive failure. Point `backupDir` to separate storage, such as a redundant array or a network share.

If you change `backupDir` on an existing install, stop qui and move the contents of `<dataDir>/backups` into the new directory. If you do not move the files, you cannot restore old backup runs and their downloads remain incomplete.

## Restore modes

If you enable backups for an instance, each run in the **Backup history** list has a restore icon button (tooltip: **Restore torrents from this backup**). The header also has a **Restore from latest** button. Restores support three modes:

### Incremental

This mode is the safest option. It creates categories, tags, and torrents that are missing from the live instance. It never modifies or deletes existing data. If you want to add missing items to an active qBittorrent instance without a change to existing data, use this mode.

### Overwrite

This mode performs the incremental restore **and** updates existing resources to match the snapshot. For example, it adjusts category save paths and rewrites per-torrent categories and tags. It does not delete any data. If your live instance has drifted and you do not want to delete items, use this mode.

### Complete

This mode performs full reconciliation. It runs the overwrite steps and deletes categories, tags, and torrents absent from the snapshot. If you want to roll an instance back to an earlier point in time and the snapshot is authoritative, use this mode.

## Preview before restore

Every restore starts with a dry-run preview so you can inspect planned changes. qui displays unsupported differences, such as mismatched infohashes or file sizes, as warnings. These warnings require manual follow-up in all modes.

## Importing backups

If you need to migrate to a new server or recover after data loss, you can import a downloaded backup into any qui instance. Click **Import backup** on the Backups page and select the backup file. qui supports all export formats.
