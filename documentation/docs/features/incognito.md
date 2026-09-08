---
sidebar_position: 18
title: Incognito Mode
description: Disguise torrent names for screen sharing, with optional spreadsheet themes that disguise the whole app.
---

# Incognito Mode

Incognito mode disguises your torrent data so you can share your screen. qui replaces torrent names with Linux ISO names. It also replaces categories, tags, trackers, and save paths with matching fake values.

The disguise is display-only. qui does not change any data in qBittorrent. Each torrent maps to the same fake values every time, based on its info hash. The pool of fake names is small, so two torrents can show the same fake name.

## Turn it on

Incognito mode is one global toggle:

- On the torrent list, click the eye icon in the status bar at the bottom. The label reads **Incognito off** or **Incognito on**.
- On mobile, tap the **Incognito** button in the torrent view.
- The eye icons next to instance URLs on the Dashboard and under **Settings > Instances** toggle the same setting.

qui saves the toggle with your client settings and syncs it to the server. The setting applies across tabs and browsers, and it is off by default.

## What it hides

| Data | Shown instead |
|------|---------------|
| Torrent names | Linux ISO names, for example `debian-12.7.0-amd64-DVD-1.iso` |
| Categories | Linux-themed categories, for example `distributions` and `server-editions` |
| Tags | Linux-themed tags, for example `lts` and `rolling-release` |
| Trackers | Linux mirror domains, for example `mirrors.kernel.org` |
| Save paths | Linux paths, for example `/home/downloads/distributions` |
| File and folder names | Linux ISO and distribution names |
| Torrent metadata | Fake "Created by", comment, and info hash |
| Peer addresses | `192.168.x.x` with masked ports |
| Ratios and sidebar filter counts | Deterministic fake numbers |

qui blurs some values instead of replacing them:

- Instance URLs and usernames on instance cards
- External IP addresses in the status bar and on the Dashboard
- Instance hosts and proxy URLs under **Settings > Client Proxy**

## What it does not hide

Sizes, speeds, progress, states, and dates stay real. Incognito mode is a visual disguise for casual viewers, not a privacy control.

:::warning
If you export a `.torrent` file while incognito mode is on, qui renames the downloaded file with the fake name. The file still contains the real torrent data. Do not share exported files as if they were disguised.
:::

## Spreadsheet disguise themes

The **Spreadsheet** and **Spreadsheet Classic** themes make qui look like a spreadsheet app. Spreadsheet Classic renders a 2003-era look and is light-only. Both are premium themes and need the same license that unlocks [custom themes](./custom-themes.md).

Select them under **Settings > Premium Themes**. While one is active, qui renders spreadsheet chrome:

| Element | What it does |
|---------|--------------|
| Ribbon tabs | Tabs such as **File**, **Home**, and **Data** across the top. Each tab is a real navigation target: **File** opens Settings, **Home** opens your first active instance, and the rest map to qui pages. Spreadsheet Classic renders a 2003-era menu bar with different names. There, **Data** opens your first active instance. |
| Formula bar | The name box shows the cell reference `A1`, or the selected row count such as `3R`. The `fx` input filters the torrent list, the same as the header search. |
| Sheet tabs | Tabs at the bottom, one per active instance. The **+** tab opens instance management. |
| Tab title | The browser tab reads `Book1.xlsx` (`Book1.xls` for Classic) and the favicon becomes a spreadsheet grid. |
| Renamed labels | Navigation entries, table headers, and filter labels use office words: Seeds becomes Sources, Peers becomes Links, Ratio becomes Yield, and ETA becomes Due. These strings stay English in every UI language. |

The spreadsheet chrome only renders on desktop-width screens. The ribbon tabs render on every page. The formula bar and the sheet tabs render only on the torrent list. Dialogs, configuration pages, and toasts keep their real names.

While a spreadsheet theme is active, incognito mode switches its vocabulary from Linux ISOs to office documents. Torrent names become spreadsheet files, for example `Q1 Budget Consolidation v3.xlsx`. Categories become departments such as `Finance`, and trackers become internal file servers such as `fileserver-01.corp.internal`.

:::note
A spreadsheet theme disguises the app chrome, not your data. Real torrent names stay visible until you also turn on incognito mode.
:::
