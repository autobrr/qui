---
sidebar_position: 15.5
title: Statistics and transfer history in qui
sidebar_label: Statistics
description: "qui records the upload and download of each torrent in its own database. Turn the recording on or off, select how long qui keeps the detail, and see how much space the history uses."
---

# Statistics

qui records the transfer history of each instance in its own database. The transfer history is the upload and download that qui recorded for each period, torrent, tracker, and category. qui keeps the bytes of a torrent after you delete the torrent. Recording starts when you upgrade qui, and it runs with no browser open.

## Statistics settings

Open **Settings → Statistics** to control the recording. The settings apply to all instances.

### Record transfer history

This switch stops and starts the recording. Recording is on by default.

While the switch is off, qui does not read the transfer of your instances, and it keeps the history that it stored. That time is a Recording gap. When you turn the switch on again, qui puts the bytes that your torrents moved during the gap on the first hour after the gap. qui also stores the length of the gap, so the Statistics page can mark that day.

### Keep daily detail for

This setting tells qui how long to keep the detail of the history. The values are 90 days, 400 days, and forever. The default is 400 days.

The detail is the upload and download of each tracker for each hour, and of each torrent for each day. After the time that you select, qui folds the old detail into larger rows:

- The hours of a day become one row for that day.
- The days of a torrent become one row for each year.

A fold does not change the total of a whole UTC day. A period that starts or ends inside a folded day counts the whole day. A chart of hours shows one bar for each day in a folded period. qui folds at the first reading of each new UTC day and at the first reading after qui starts. When you select a shorter time, the next reading folds.

qui keeps these rows forever: the daily totals of each tracker and category, the yearly rows of each torrent, the record of each torrent, and the events for adds, completions, and deletes.

### History size

The section shows how much space the history uses in the database. qui measures it at most one time each hour, and again after a fold.

On SQLite, a fold does not make the database file smaller. SQLite uses the free space again for new rows. After the retention time, each day deletes about as many rows as it adds, so the detail stops growing.

An API key needs **Settings: write** to change these settings. A key with an instance limit cannot change them.
