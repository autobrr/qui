---
sidebar_position: 14
title: Torrent Management
description: Tags, categories, saved filter views, keyboard control, torrent creation, export, and MediaInfo.
---

# Torrent Management

qui provides tools to manage torrent lists: tags, categories, saved filter views, keyboard control, a torrent creator, `.torrent` export, and MediaInfo. For queue, speed, and share limits, see [qBittorrent Preferences](./instance-settings.md#qbittorrent-preferences).

## Tags and categories

Manage tags and categories from the **Tags** and **Categories** sections in the filter sidebar. Open the context menu on a tag or category to see its actions.

- **Create tag** adds one tag to the instance.
- **Delete Tag** removes a tag from the instance.
- **Delete unused tags** lists every tag with zero torrents and removes them in one step.
- **Create category** adds a category with an optional save path.
- **Create Subcategory** adds a child category. The dialog pre-fills the name with `parent/`.
- **Edit Category** changes the save path of a category. The name stays fixed.
- **Delete Category** removes one category.
- **Remove Empty Categories** lists every category with zero torrents and removes them in one step.

### Subcategories

qBittorrent stores a subcategory as a name with `/` separators, for example `media/movies`. When subcategories are active, qui shows the categories as a collapsible tree sorted by name. Subcategories require qBittorrent WebUI API 2.9.0 or newer. On older versions, qui shows a flat list.

## Saved filter views

The **Views** section in the filter sidebar saves the current filter selection as a named view.

1. Select filters in the sidebar.
2. Click **Save current filters**.
3. Enter a name and click **Save**.

Click a saved view to apply its filters. qui highlights the active view. Each view has a menu with three actions:

- **Update filters** overwrites the view with the current sidebar selection. The confirmation toast has an **Undo** action that restores the previous filters.
- **Rename** changes the name of the view.
- **Delete** removes the view.

A view stores the sidebar filter selection only. It does not include search text, sorting, or column filters. Names must be unique and have a limit of 100 characters.

## Selection and keyboard control

| Input | Action |
|-------|--------|
| Click | Opens the details panel for the row |
| Ctrl/Cmd + click | Toggles selection of the row |
| Shift + click | Selects the range from the last selected row |
| Ctrl/Cmd + A | Selects all torrents in the current view |
| Arrow Up / Arrow Down | Moves the selection one row |
| Enter | Opens the details panel for the focused row |
| Escape | Closes the details panel |

The arrow keys replace the selection with the focused row, the same as a plain click. When the details panel is open, it follows the arrow keys. If you type in an input field or open a dialog, hotkeys are inactive.

## Torrent creator

The **Create torrent** button in the header opens the creator dialog. It creates a new `.torrent` file from a file or folder on the qBittorrent server. If the instance supports torrent creation, the button appears in the header. Torrent creation requires qBittorrent 5.0 or newer (WebUI API 2.11.2).

| Field | Description | Default |
|-------|-------------|---------|
| Source Path | Full path on the qBittorrent server. Suggestions appear as you type. | Required |
| Private torrent | Disables DHT, PEX, and local peer discovery | On |
| Trackers | Pick from active trackers or enter one URL per line | - |
| Comment | Free-text comment stored in the torrent | - |
| Source | Source field stored in the torrent | - |
| Add to qBittorrent | Adds the created torrent and starts seeding. When off, qui only creates the file for download. | On |
| Torrent Format | `v1 (Compatible)`, `v2 (Modern)`, or `Hybrid (v1 + v2)` | v1 |
| Piece Size | Piece size for the torrent | Auto |
| Save .torrent to (optional) | Full file path on the server for the created file. A directory alone is invalid. | - |
| Web Seeds (HTTP/HTTPS) | URLs where clients download content, one per line | - |

:::note
Hybrid and v2 formats require a qBittorrent build with libtorrent 2. On a libtorrent 1.x build, qui creates v1 torrents only.
:::

### Creation tasks

The **Torrent Creation Tasks** list in the header shows every creation job with its status: Queued, Running, Finished, or Failed. Running tasks show a progress bar. While tasks run, the list refreshes every 2 seconds. From a finished task, you can download the `.torrent` file or delete the task.

## Torrent export

**Export Torrent** in the torrent context menu downloads the `.torrent` file for the selected torrent. With multiple torrents selected, the action reads **Export Torrents (N)**. Export requires qBittorrent WebUI API 2.8.11 or newer.

Up to 10 torrents download as individual files. Larger selections download as one `qui-torrents.zip` archive. Inside the archive, qui groups files into folders by category. qui places torrents without a category into `Uncategorized`. When the selection spans multiple instances, each instance gets its own top-level folder.

If some torrents fail to export, the archive still downloads. An `export-errors.txt` file inside the archive lists the failures.

## MediaInfo

The **Content** tab of the details panel offers a **MediaInfo** action on each file. It analyzes the file on disk and opens a dialog with two tabs: **Summary** and **Raw JSON**. **Copy Summary** and **Copy JSON** copy the report to the clipboard.

Because qui reads the file from disk, MediaInfo requires the instance option **Local Filesystem Access**. See [Instance Settings](./instance-settings.md#local-filesystem-access).