---
sidebar_position: 15.5
title: qBittorrent transfer statistics in qui
sidebar_label: Statistics
description: "The Statistics page of qui: upload and download for each period, by tracker, from the transfer history that qui records. Turn the recording on or off, and select how long qui keeps the detail."
---

# Statistics

The Statistics page shows how much you uploaded and downloaded in a period. qui gets the numbers from its own transfer history. To open the page, click **Statistics** in the sidebar. On a phone, tap **Statistics** in the bottom bar.

## How qui records the transfer

qui records the transfer of each torrent from the moment that you upgrade to a version with Statistics. You do not have to turn it on. To stop the recording, see [Statistics settings](#statistics-settings). qui records with no browser open, from the torrent data that it already reads from qBittorrent. It sends no extra requests to qBittorrent.

qui keeps the transfer of a torrent after you delete the torrent. It also keeps the history of an instance after you remove the instance, under the name of the instance. To delete the history together with the instance, tick **Also delete its transfer history** in the dialog that removes the instance. The tick box is off by default.

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

## Statistics settings

Open **Settings → Statistics** to control the recording. The settings apply to all instances.

### Record transfer history

This switch stops and starts the recording. Recording is on by default.

While the switch is off, qui does not read the transfer of your instances, and it keeps the history that it stored. That time is a Recording gap. When you turn the switch on again, qui puts the bytes that your torrents moved during the gap on the first hour after the gap. qui also stores the length of the gap, so the Statistics page can mark that day. A torrent that someone deletes while the switch is off shows the cause "outside qui", because qui does not keep track of deletes during that time.

### Keep daily detail for

This setting tells qui how long to keep the detail of the history. The values are 90 days, 400 days, and forever. The default is 400 days.

The value sets how far back a chart of hours shows one bar for each hour. 400 days covers a full year and one more month. 90 days covers a 30-day period and the 30 days before it, and the history uses less space. With forever, the history grows each day.

The detail is the upload and download of each tracker for each hour, and of each torrent for each day. After the time that you select, qui folds the old detail into larger rows:

- The hours of a day become one row for that day.
- The days of a torrent become one row for each year.

A fold does not change the total of a whole UTC day. A folded day is a UTC day, so its bar starts at midnight UTC, not at midnight in the timezone of your browser. A period that starts or ends inside a folded day counts the whole day. A chart of hours shows one bar for each day in a folded period. qui folds at the first reading of each new UTC day and at the first reading after qui starts. When you select a shorter time, the next reading folds. You cannot get folded detail back.

qui keeps these rows forever: the daily totals of each tracker and category, the yearly rows of each torrent, the record of each torrent, and the events for adds, completions, and deletes.

### History size

The section shows how much space the history uses in the database. qui measures it at most one time each hour, and again after a fold.

On SQLite, a fold does not make the database file smaller. SQLite uses the free space again for new rows. After the retention time, each day deletes about as many rows as it adds, so the detail stops growing.

An API key needs **Settings: write** to change these settings. A key with an instance limit cannot change them.

### Delete history

**Delete history** deletes the transfer history of one instance or of all instances. The list of instances also holds each removed instance that has history. You cannot get deleted history back.

1. Select the instance, or **All instances**.
2. Select **Everything**, or select **Everything before a date** and the date.
3. Click **Delete history**. The dialog names the instance and the range.
4. Click **Delete**.

After a delete of everything, qui starts to record the instance again. The torrents that are in qBittorrent at that time go into a new Baseline, and "Recording since" shows the new date. A removed instance has no history after the delete.

A delete before a date keeps each hour and each day from that date on. qui stores days as UTC days, so it keeps the whole UTC day that holds the start of your date. If recording started before the date, the Baseline goes too, and "Recording since" moves to the start of that UTC day. In a timezone east of UTC, that is the day before your date. The yearly row of a torrent goes when its year ends before the date.

qui deletes in a series of short writes, so the other features of qui can write to the database between them. The Statistics page waits until the delete ends. qui records the transfer of that time when the delete ends.

On SQLite, the database file keeps its size after a delete. SQLite uses the free space again for new history. On Postgres, a delete of everything of all instances gives the space back to the disk at once. After another delete, Postgres uses the free space again.

An API key needs **Settings: write** to delete history. A key with an instance limit cannot delete history.

The tick box in the dialog that removes an instance sends a second request after the removal. If that request fails, qui removes the instance and keeps its history. You can then delete the history here.

## Why the numbers differ from Server Statistics

Server Statistics on the [Dashboard](./dashboard.md) reads the all-time counters of qBittorrent. These counters also count protocol traffic. The Statistics page adds up the transfer of each torrent. Thus, the two pages show different numbers.

## Incognito mode

If [incognito mode](./incognito.md) is on, the page replaces the tracker names with fake names.
