---
sidebar_position: 17
title: RSS
description: Manage qBittorrent RSS feeds and auto-download rules.
---

# RSS

The RSS page manages the native RSS system inside qBittorrent. You can add feeds, organize them in folders, read articles, and create auto-download rules. qui writes all changes to qBittorrent, so they also apply in the qBittorrent WebUI.

Select an instance at the top of the page. The page shows the feeds and rules for that instance only.

:::note
This page is not the cross-seed RSS automation. For automatic cross-seeding from indexer RSS feeds, see [Cross-Seed](./cross-seed/overview.md).
:::

If you disable RSS fetching or auto-downloading in qBittorrent, qui shows a warning banner. Click **Enable RSS** or **Enable Auto-Download** in the banner to turn the function on.

## Feeds

The **Feeds** tab shows your feeds in a tree on the left and the articles of the selected feed on the right. Folders show their unread count, or the number of feeds inside when nothing is unread. **Refresh All** triggers a refresh of every feed. While the tab is open, qui streams feed updates live and shows the connection state in a status badge.

### Add a feed

1. Click **Add Feed**.
2. Enter the feed URL. The URL must start with `http://` or `https://`.
3. If you want to place the feed in a folder, select one from the **Folder** list.
4. Click **Add Feed**.

:::note
qBittorrent adds each new feed to the root first. qui then moves the feed into the selected folder. If the move fails, the feed stays in the root and qui shows a warning.
:::

### Folders

Click **Add Folder** to create a folder. If you want to nest folders, put a backslash in the folder path, for example `Shows\Anime`.

### Feed actions

Each feed in the tree has a menu with these actions:

| Action | What it does |
|--------|--------------|
| Refresh | Triggers a refresh of this feed |
| Rename | Renames the feed or folder |
| Edit URL | Changes the feed URL and keeps the feed |
| Open URL | Opens the feed URL in a new browser tab |
| Remove | Deletes the feed from qBittorrent |

:::note
**Edit URL** needs qBittorrent Web API 2.9.1 or later. qui hides the action on older versions.
:::

### Articles

The articles panel lists the articles of the selected feed, newest first. The search box filters articles by title and description. Expand an article to read its description.

Each article has these actions:

- **Download torrent** opens the Add Torrent dialog and fills in the torrent URL.
- **Open link** opens the article link in a new browser tab.
- **Mark as read** clears the unread state of one article. **Mark all as read** clears the whole feed.

## Auto-download rules

The **Rules** tab lists the auto-download rules of the instance. qBittorrent matches enabled rules against new articles and downloads matching torrents. Each rule row has a toggle to enable or disable the rule, and shows a filter summary, category, and target feed count.

### Rule configuration

Click **Add Rule** to create a rule, or click the pencil icon to edit one. A rule has these fields:

| Field | Description |
|-------|-------------|
| Rule Name | Required. The name qBittorrent stores the rule under. You cannot change it after you create the rule. |
| Must Contain | Text the article title must include, for example `keyword1\|keyword2` |
| Must Not Contain | Text that excludes an article |
| Episode Filter | Season and episode ranges in the format `S01-S03;E01-E10` |
| Use Regex | Treats the contain filters as regular expressions |
| Smart Episode Filter | Tracks matched episodes and skips repeated downloads of the same episode |
| Affected Feeds | The feeds the rule applies to |
| Save Path | Save location for matched torrents |
| Category | Category for matched torrents |
| Tags | Tags for matched torrents |
| Ignore subsequent matches for | Days to ignore new matches after a match, default 0 |
| Torrent Content Layout | Original, create subfolder, no subfolder, or the global qBittorrent value |
| Add Stopped | Adds matched torrents in the stopped state, or uses the global value |

If a rule matched before, the edit dialog shows the **Last Match** date.

### Preview matching articles

Click the magnifier icon on a rule to open **Matching Articles**. The panel groups matching article titles by feed. If you want to test a filter before you enable the rule, use this preview.

### Reprocess rules

Click **Reprocess** to check all unread articles against all rules again. If you create or edit a rule, use this action so qBittorrent evaluates articles that arrived before the change.

## RSS settings

The gear button next to the instance selector opens the **RSS Settings** popover:

| Setting | Description |
|---------|-------------|
| Refresh interval | Minutes between automatic feed refreshes |
| Max articles per feed | Number of articles qBittorrent keeps per feed |
| Download REPACK/PROPER | Also downloads REPACK and PROPER versions of matched episodes |

These values are qBittorrent preferences. A change applies to the whole instance.
