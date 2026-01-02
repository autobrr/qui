---
sidebar_position: 1
title: Backups
description: Schedule and restore qBittorrent instance backups.
---

# Backups & Restore

qui can take scheduled or ad-hoc snapshots of a qBittorrent instance. Each snapshot includes the torrent archive, tags, categories (with save paths), and cached `.torrent` blobs so that you can recreate the original state later.

## Restore Modes

Once backups are enabled for an instance the backlog UI exposes a **Restore** action for each run. Restores support three distinct modes:

### Incremental

Safest option. Creates any categories, tags, or torrents that are missing from the live instance but never modifies or removes existing data. Use this when you just want to seed new items into an active qBittorrent without touching what is already there.

### Overwrite

Performs the incremental work **and** updates existing resources to match the snapshot (e.g. adjusts category save paths or rewrites per-torrent categories/tags). It still refuses to delete anything. This works well when your live instance has drifted but you do not want to prune it.

### Complete

Full reconciliation. Runs the overwrite steps and then deletes categories, tags, and torrents that are not present in the snapshot. This is ideal when you need to roll an instance back to an earlier point in time, but it should only be used when you are certain the snapshot is authoritative.

## Preview Before Restore

Every restore begins with a dry-run preview so you can inspect planned changes. Unsupported differences (such as mismatched infohashes or file sizes) are surfaced as warnings; they require manual follow-up regardless of mode.

## Importing Backups

Downloaded backups can be imported into any qui instance. Useful for migrating to a new server or recovering after data loss. Click **Import** on the Backups page and select the backup file. All export formats are supported.
