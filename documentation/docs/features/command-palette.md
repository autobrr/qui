---
sidebar_position: 14.5
title: Command palette for keyboard navigation in qui
sidebar_label: Command Palette
description: Open a page, a settings section, an instance, or a torrent from the keyboard with Cmd + K or Ctrl + K, or from the Search button on a phone.
---

# Command Palette

The command palette is a search field above a list of destinations. With it, you open a page, a section of a page, an instance, or a torrent from the keyboard.

## Open the palette

Press Cmd + K on macOS, or Ctrl + K on Windows and Linux. The same keys close the palette.

The palette opens on each page of qui. It does not open while a dialog is open, because a change of page discards the text that you typed in the dialog.

A phone has no such keys. See [Use the palette on a phone](#use-the-palette-on-a-phone).

## Find a destination

Type some letters of a name, then press Enter. The palette searches these groups together:

| Group | Entries |
|-------|---------|
| Instances | Each active instance, with its [icon](./instance-settings.md#instance-icons) and its connection state. If you have two or more active instances, the **Unified** list of all instances comes first. |
| Pages | The pages of the sidebar, and the sections of **Settings**, **Cross-Seed**, **Automations**, and **RSS**. A section shows its page below its name, for example **Settings › Connections**. |
| Actions | **Add torrent**, the incognito mode switch, and **Theme**. See [Change the theme](#change-the-theme). |
| Torrents | The torrents that match the text. See [Find a torrent](#find-a-torrent). |

A row with an arrow on the right has a second list. For a page, the second list holds its sections. For an instance, it holds **Torrents**, which opens the torrent list of that instance, and **Add torrent**. For **Theme**, it holds the modes and the themes.

| Key | Action |
|-----|--------|
| Up and Down | Move the highlight |
| Enter | Open the highlighted row |
| Cmd/Ctrl + Enter | Filter a torrent list with the text that you typed, also when another row has the highlight. See [Find a torrent](#find-a-torrent). |
| Right | Show the second list of the highlighted row |
| Backspace or Left, in an empty search field | Go back to the first list |
| Cmd/Ctrl + 1 to 9 | Open the instance that shows this shortcut |
| Cmd/Ctrl + 0 | Open the **Unified** list |
| Escape | Close the palette |

On macOS, Ctrl + J and Ctrl + N also move the highlight down, and Ctrl + K and Ctrl + P move it up.

## Find a torrent

When the text has two characters or more, the palette searches one torrent list. If a torrent list is open, the palette searches that list. On another page, it searches the **Unified** list. If you have one active instance, it searches the list of that instance.

The search matches names, categories, tags, and info hashes, the same as the search field of the torrent list. It accepts the same glob patterns, for example `*.mkv` or `*1080p*`.

The filters of that list apply to the search. These are the sidebar filters and, in the table, the column filters. The palette thus finds only the torrents that the list shows for the text. If a filter is on, the row **Search torrents for "text"** shows **List filters apply**. To find a torrent that a filter hides, first [clear the filters](./torrent-management.md#clear-filters) of the list.

The palette shows the six torrents that you added last. Each row shows the state, the size, and the tracker of the torrent. In the **Unified** list, the row also shows the instance. The copies of a release share one row, and a number such as **+2** shows how many other copies the row holds.

- Press Enter on a torrent to open it in the torrent list. The list then shows the torrents that match the text, as with **Search torrents for "text"**. The table scrolls to the torrent, highlights it, and opens its details panel.
- Press Cmd + Enter on macOS, or Ctrl + Enter on Windows and Linux, to filter the list with the text. Ctrl + Enter also works on macOS. The highlight can be on any row.
- The row **Search torrents for "text"** does the same when you press Enter on it. This row also shows the number of torrents that match. If you have two or more active instances, it shows the name of the list.

[Incognito mode](./incognito.md) also replaces the torrent names in the palette.

## Change the theme

The **Theme** row shows the theme and the mode in use. Press Enter on this row, or tap it, to show its second list:

| Group | Entries |
|-------|---------|
| Mode | **Light**, **Dark**, and **System**. With **System**, qui follows the light or dark setting of your device. |
| Theme | Every theme, also the themes that the theme menu does not show. Each row shows a small picture of qui in the colors of the theme. A theme that you cannot apply shows **Needs a license**. The highlight skips that row. |

Press Enter on a mode or on a theme to apply it. The mode and the theme in use show **Current**.

A search from the first list finds the three modes. For example, type "dark", then press Enter. It does not find a theme by its name. This keeps the theme names out of the results when you search for a torrent. To find a theme, show the second list of **Theme**, then type a part of its name.

The palette does not change the color variation of a theme. For that, see [The theme picker](./custom-themes.md#the-theme-picker).

## Use the palette on a phone

Tap **Search** in the bottom bar. The bar has this button on each page, between **Statistics** and the instances.

If the torrent list has a search text, the palette opens with that text. The palette selects the text, so the first character that you type replaces it. To add to the text, first tap at its end.

On a touch screen, the palette shows no keys, and these rules apply:

- The row **Search torrents for "text"** is the first row. Type the text, then tap this row or the Enter key of the on-screen keyboard. The list then shows only the torrents that match.
- If no torrent matches the text, the destinations come first. The Enter key then opens the first destination. If an instance does not answer the search and no other instance has a match, the row **Search torrents for "text"** shows **The torrent search failed** and stays first. The palette searches again every 3 seconds until each instance answers.
- To open a different row, tap it.
- To see the second list of a row, tap the arrow on its right. To go back, tap the arrow next to the search field.
- To close the palette, tap the X or tap outside the palette.

To remove the filter from the list, tap **Clear** on the bar that shows the text above the cards.

## Search field of the torrent list

On a torrent list, press / to move the cursor to the search field of the list. Before the command palette, Cmd + K and Ctrl + K did this.
