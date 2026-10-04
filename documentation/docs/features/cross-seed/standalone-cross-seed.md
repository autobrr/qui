---
sidebar_position: 10
title: Move from standalone cross-seed
sidebar_label: From standalone cross-seed
description: Run qui cross-seed next to the standalone cross-seed app, or replace it, and map its settings to qui.
---

# Move from Standalone Cross-Seed

qui cross-seed does not need the standalone [cross-seed](https://www.cross-seed.org/) app. This page tells you what happens when both run on one qBittorrent instance, and how to move your setup to qui.

## Run Both at the Same Time

You can run both tools on the same qBittorrent instance. They do not add the same torrent two times:

- Each tool checks the infohash before it adds a torrent. If the torrent is already in qBittorrent, the tool does not add it, no matter which tool added it first.
- qBittorrent also refuses a second torrent with the same infohash.
- qui does not search a tracker for a torrent when the instance already seeds that content from that tracker. This includes torrents that the standalone app added.

Each tool limits only its own requests. When both run, Prowlarr, Jackett, and your trackers get the searches from both tools. If a tracker limits your API use, run only one of the tools, or make the searches less frequent in one of them.

Both tools add the tag `cross-seed` by default. qui does not start a completion search for a torrent that has this tag, so torrents that the standalone app added do not start new qui searches when they complete.

The default categories are different. qui adds `.cross` to the category of the matched torrent. The standalone app keeps the category of the matched torrent, adds `.cross-seed` when `duplicateCategories` is on, or uses `cross-seed-link` for linked torrents. With linking on, `duplicateCategories` adds the marker as a tag instead.

## Move to qui

qui cannot import the settings or the history of the standalone app. Do these steps:

1. Add your indexers in **Settings → Indexers**. Click **Discover** to import them from Prowlarr or Jackett. See [Add indexers](../search.md#add-indexers).
2. If you used `linkDirs`, turn on hardlink or reflink mode for each instance and set the base directories. See [Hardlink mode](./hardlink-mode.md) and [Link directories](./link-directories.md).
3. If autobrr sends announces to the standalone app (`/api/announce`), change the autobrr action to the qui endpoints. See [autobrr integration](./autobrr.md).
4. Set the rules and the discovery methods in qui. Use the table below.
5. Stop the standalone app.

You do not have to remove anything. The torrents that the standalone app added continue to seed. qui does not manage or remove the link directories of the standalone app.

## Settings

| Standalone setting | qui setting |
|---|---|
| `torznab` | **Settings → Indexers** |
| `linkDirs`, `linkType` | Hardlink or reflink mode for each instance. qui has no symlink mode. |
| `flatLinking` | **Directory organization**: `flat`, `by-tracker`, or `by-instance` |
| `matchMode` | No setting. qui accepts matches with extra files and rechecks them. Dir Scan has **Allow partial matches**. |
| `autoResumeMaxDownload` | **Max auto-start download** in [rules](./rules.md) |
| `skipRecheck` | **Skip recheck** in [rules](./rules.md). The meaning is different, see below. |
| `rssCadence` | RSS Automation **Run interval** |
| `delay`, `excludeRecentSearch` | Library Scan interval and **Cooldown** |
| `includeSingleEpisodes` | Turn Library Scan **Skip individual episodes** off to include episodes. For episodes from season packs, use **Cross-seed episodes from packs** in [rules](./rules.md). |
| `seasonFromEpisodes` | [Season pack assembly](./season-packs.md) |
| `dataDirs` | [Dir Scan](./dir-scan.md) |
| `duplicateCategories` | **Category Affix** in [rules](./rules.md) |
| `blockList` | [Blocklist](./overview.md#blocklist) for each instance. It accepts infohashes only. |

:::warning Skip recheck is not the same setting
In the standalone app, `skipRecheck` adds the torrent without a recheck, except for partial matches and disc layouts. In qui, **Skip recheck** does not add a cross-seed that needs a recheck.
:::

qui has no equivalent for these standalone settings: symlink links, `action: save`, `excludeOlder`, `searchCadence`, and blocklist entries by name, tracker, or size.

A Library Scan runs only when you start it. For continuous discovery, use RSS Automation, Auto-Search on Completion, or [autobrr](./autobrr.md).

The standalone app also supports torrent clients other than qBittorrent. qui supports only qBittorrent.
