---
sidebar_position: 15.5
title: qBittorrent transfer statistics in qui
sidebar_label: Statistics
description: "The Statistics page of qui: upload and download for each period, by tracker, from the transfer history that qui records."
---

# Statistics

The Statistics page shows how much you uploaded and downloaded in a period. qui gets the numbers from its own transfer history. To open the page, click **Statistics** in the sidebar. On a phone, tap **Statistics** in the bottom bar.

## How qui records the transfer

qui records the transfer of each torrent from the moment that you upgrade to a version with Statistics. You do not have to turn it on. qui records with no browser open, from the torrent data that it already reads from qBittorrent. It sends no extra requests to qBittorrent.

qui keeps the transfer of a torrent after you delete the torrent. It also keeps the history of an instance after you remove the instance.

The page shows "Recording since" with the date of the first recording of the selected instances.

## Period

Select the period at the top of the page:

| Period | Bars of the chart |
|--------|-------------------|
| 24 hours | One bar for each hour |
| 7 days | One bar for each hour |
| 30 days | One bar for each day |
| Year | One bar for each day |
| All time | One bar for each day, from the first recording |
| Custom | One bar for each hour for 7 days or less, and one bar for each day for a longer range |

A day starts at midnight in the timezone of your browser.

While the page stays open and in view, it moves a period that ends now forward each 5 minutes and loads the new transfer. qui writes the transfer each 5 minutes. A custom range does not move.

If a period starts before the first recording of a selected instance, the page shows the label "Partial period". The numbers of such a period are too small, because qui did not record the start of the period for that instance.

## Instances

If you have two or more instances with history, select **All instances** or a set of instances. The page adds the selected instances together. A removed instance that has history is in the list with the mark "removed".

## Numbers

| Number | Content |
|--------|---------|
| Uploaded | The upload of the period |
| Downloaded | The download of the period |
| Ratio | Uploaded divided by Downloaded. Below the ratio, the buffer is Uploaded minus Downloaded |
| From cross-seeds | The upload of the torrents that completed with less than 1% of their size downloaded, and its share of the upload |

If qui recorded the period before in full, Uploaded and Downloaded also show the change against that period. qui stores the transfer of each whole hour, so the change leaves out the hour that still runs. The period before has the same length as the rest of the selected period. It ends at the start of the hour in which the selected period starts.

## Chart

The chart shows the upload or the download of each bar. Use the switch above the chart to select **Upload** or **Download**. The bars stack by tracker. The five trackers with the most bytes in the period each get a color. The other trackers are in the gray series Other.

The page counts each torrent toward one tracker. Thus, the tracker series of the chart add up to the Uploaded or Downloaded number of the period. All time is the exception, because its numbers also hold the Baseline. A torrent that is not private counts toward Public. A torrent with no tracker counts toward No tracker. Tracker customizations apply: a merged tracker is one series under its display name. A rename or a merge also changes the past.

## Baseline

When qui first records an instance, the torrents already have upload and download. These bytes are the Baseline. The Baseline has no date.

- The numbers of All time include the Baseline. On the first day, the all-time numbers agree with the lifetime numbers of your torrents.
- The chart does not show the Baseline, because it has no date.
- The other periods do not include the Baseline.

## Recording gap

A recording gap is a time when qui did not read an instance, for example while qui was stopped. qui does not lose the transfer of that time. It adds the transfer to the first hour after the gap, so that hour can show a tall bar.

The chart marks each bar that includes a recording gap with a dashed yellow line. The label above the line gives the length of the gap.

## Why the numbers differ from Server Statistics

Server Statistics on the [Dashboard](./dashboard.md) reads the all-time counters of qBittorrent. These counters also count protocol traffic. The Statistics page adds up the transfer of each torrent. Thus, the two pages show different numbers.

## Incognito mode

If [incognito mode](./incognito.md) is on, the page replaces the tracker names with fake names.
