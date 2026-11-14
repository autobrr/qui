---
sidebar_position: 15.5
title: qBittorrent transfer statistics in qui
sidebar_label: Statistics
description: "The Statistics page of qui: upload and download for each period, by tracker, category, or release attribute, from the transfer history that qui records. Turn the recording on or off, and select how long qui keeps the detail."
---

# Statistics

The Statistics page shows how much you uploaded and downloaded in a period. qui gets the numbers from its own transfer history. To open the page, click **Statistics** in the sidebar. On a phone, tap **Statistics** in the bottom bar.

## How qui records the transfer

qui records the transfer of each torrent from the moment that you upgrade to a version with Statistics. You do not have to turn it on. To stop the recording, see [Statistics settings](#statistics-settings). qui records with no browser open, from the torrent data that it already reads from qBittorrent. It sends no extra requests to qBittorrent.

qui also records the size of your library and the state of each instance. For each day, it stores the torrent count, the total size with each torrent counted in full, and the [Unique size](./dashboard.md#unique-size) of the last reading of the day. A day without a completed Copy groups build has no Unique size. For each hour, it stores the torrent count of each torrent state, the free disk space that qBittorrent reports, and the highest upload and download speed that qui saw.

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

## Table

The table below the chart shows the numbers of the period for each tracker, each category, or each value of a release attribute. Use the **Group by** switch above the table to select **Tracker**, **Category**, **Release group**, **Resolution**, **Source**, or **Content type**.

### By tracker

The table has one row for each tracker, with these columns:

| Column | Content |
|--------|---------|
| Uploaded | The upload of the period |
| Downloaded | The download of the period |
| Ratio | Uploaded divided by Downloaded |
| Buffer | Uploaded minus Downloaded |
| Torrents | The number of torrents of the tracker that are in qBittorrent now |
| Size | The size of those torrents. Torrents of the tracker that share one content path count once |
| Seeded | The lifetime upload of those torrents divided by their size |

Uploaded, Downloaded, Ratio, and Buffer are for the selected period. Torrents, Size, and Seeded are live numbers. They describe the torrents that are in qBittorrent now, whatever the period. A removed or disabled instance has no live numbers.

The table counts each torrent toward one tracker, as the chart does. A torrent with three tracker domains counts one time. Thus, the rows add up to the numbers at the top of the page, and the Total row shows that sum. For a session total, select the period 24 hours.

### By category

The table has one row for each category, with Uploaded, Downloaded, Ratio, and Buffer. A row holds the bytes that moved while the torrent was in that category. If you move a torrent to another category, its older bytes stay under the old category.

qui stores the totals of each category for each UTC day. A period that starts or ends inside a UTC day counts that whole day. Thus, the Total row of the categories can be larger than the numbers at the top of the page, for example for the period 24 hours. For All time, the two totals agree.

### By release attribute

**Release group**, **Resolution**, **Source**, and **Content type** group the torrents by a value that qui reads from the torrent name. The table has one row for each value, with Uploaded, Downloaded, Ratio, and Buffer. A torrent whose name has no value for the attribute is in the row **None**.

qui reads the name when it first sees a torrent, and again when the name changes. A row holds all the bytes of each torrent under the values of its name now. If you rename a torrent, its older bytes move to the new values.

qui stores the bytes of each torrent for each UTC day. As for a category, a period that starts or ends inside a UTC day counts that whole day. In a year that qui folded, a torrent has one row for the whole year. Thus, a period that overlaps a folded year counts the whole year. See [Keep daily detail for](#keep-daily-detail-for).

The upload from cross-seeds of a row holds all the upload of each cross-seed. This includes the upload from before qui marked the torrent as a cross-seed. Thus, the number can be larger than the same number in the tracker table.

To answer, qui reads the bytes of each torrent in the period. For a year, this can take a few seconds, and the table shows a spinner until the answer is ready. qui keeps the answer until the history changes, so a second look at the same period is fast. While qui records, it writes the history each 5 minutes.

### Detail of a row

Click a row to open its detail below that row. Click the row again to close it. One row is open at a time, and the table keeps all its columns. On a phone, the table scrolls sideways, and the detail stays in view at the full width of the screen.

The detail shows:

- **Share of the upload**: the part of the upload of the period that the row holds.
- **From cross-seeds**: the upload of the row from torrents that completed with less than 1% of their size downloaded, and its part of the upload of the row.
- **Best hour** or **Best day**: the bar with the most upload, and its time.
- **Average per hour** or **Average per day**: the upload of the row divided by the number of hours or days that the bars cover after the first recording.
- A chart of the row, with the upload above the zero line and the download below it.
- **Top torrents**: the five torrents of the row with the most upload in the period. A torrent counts toward the tracker and the category that it has now. Thus, a torrent that you moved to another category shows under its new category with all its upload of the period.

The chart of a tracker has the bars of the main chart: hours for a period of 7 days or less, and days for a longer period. The chart of a category or a release attribute shows days, because qui stores those bytes for each UTC day. In the chart of a release attribute, a year that qui folded is one bar. That bar is never the best day, and the average counts each day that it stands for.

For All time, the rows, the share, and the upload from cross-seeds include the Baseline. The chart, the best bar, and the average do not, because the Baseline has no date.

## Top torrents

The table below the table of trackers shows the ten torrents with the most upload in the period. It includes each selected instance. Each row has these columns:

| Column | Content |
|--------|---------|
| Torrent | The name of the torrent. The mark "Deleted" shows that the torrent is not in qBittorrent now. The mark "Cross-seed" shows that the torrent completed with less than 1% of its size downloaded |
| Tracker | The tracker that the torrent has now |
| Uploaded | The upload of the torrent in the period |
| Downloaded | The download of the torrent in the period |
| Ratio | Uploaded divided by Downloaded |
| First 24 hours | The upload of the torrent in the 24 hours after you added it. The cell is empty when qui first saw the torrent after those 24 hours |

A deleted torrent stays in the list with its name. Thus, the list of a long period is complete. The table counts all the upload of a cross-seed, also the upload from before the torrent completed. For All time, the upload includes the Baseline of each torrent. On a phone, the table hides some columns and shows the tracker below the name.

Click a row to see the upload and download of that torrent for each day of the period. qui stores the bytes of a torrent for each UTC day, so each bar is a UTC day.

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

The detail is the upload and download of each tracker for each hour, the upload and download of each torrent for each day, and the state of each instance for each hour. After the time that you select, qui folds the old detail into larger rows:

- The hours of a day become one row for that day.
- The days of a torrent become one row for each year.
- qui deletes the state of each instance for each hour: the torrent counts by state, the free disk space, and the highest speeds.

After a fold, the top torrents of an old period count the upload of each torrent in the whole folded year. For example, the top torrents of one month in a folded year show the upload of the full year. The chart of one torrent shows that year as one bar. A fold does not change the total of a whole UTC day. A folded day is a UTC day, so its bar starts at midnight UTC, not at midnight in the timezone of your browser. A period that starts or ends inside a folded day counts the whole day. A chart of hours shows one bar for each day in a folded period. qui folds at the first reading of each new UTC day and at the first reading after qui starts. When you select a shorter time, the next reading folds. You cannot get folded detail back.

qui keeps these rows forever: the daily totals of each tracker and category, the yearly rows of each torrent, the record of each torrent, the events for adds, completions, and deletes, and the library of each day.

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

On SQLite, the database file keeps its size after a delete. SQLite uses the free space again for new history. To give the space back to the disk, [shrink the database file](#shrink-the-database-file). On Postgres, a delete of everything of all instances gives the space back to the disk at once. After another delete, Postgres uses the free space again.

An API key needs **Settings: write** to delete history. A key with an instance limit cannot delete history.

The tick box in the dialog that removes an instance sends a second request after the removal. If that request fails, qui removes the instance, and a part or all of its history can stay. You can delete the rest here.

### Shrink the database file

On SQLite, the Delete history dialog has the tick box **Also shrink the database file**. With the tick, qui deletes the history, restarts, and makes the database file smaller. The disk gets the free space back. qui is unavailable while the shrink runs. The time grows with the data that stays in the file. For a large file on a slow disk, it can take some minutes. When qui is back, a message shows the size of the file before and after the shrink.

Without the tick, the file keeps its size, and SQLite uses the free space again for new history. Thus, it is safe to leave the tick box empty.

A shrink needs free disk space of up to two times the size of the database file. If the disk of the database has less free space, the tick box is off, and the dialog shows the free space and the need. You can then [shrink the file by hand](#shrink-the-database-file-by-hand), also into a file on another disk.

If qui cannot restart itself on your install, the dialog says so. The shrink then runs at the next start of qui. The shrink also runs when you restart qui in another way. qui never shrinks the file without a request.

If the shrink fails, qui starts as usual, and the database does not change. qui shows the System notice **Shrink failed** in the sidebar, in the header menu, and in the mobile menu. The notice gives the cause and the fix:

- The disk has too little free space. Free space on the disk.
- Another program held the database file, for example a backup tool or a database viewer. Stop that program.
- For another cause, the notice links to the log.

To request a new shrink and restart qui, click **Try again**. To close the notice, click **Dismiss**. qui does not try again on its own.

Only the web UI can request a shrink. An API key cannot request one, because the shrink restarts qui. On Postgres, the tick box is absent, because a delete of everything gives the space back at once.

### Shrink the database file by hand

Use these steps if the disk of the database has too little free space for a shrink. You need the `sqlite3` command-line program on the host of the database file. Replace `/config/qui.db` with the path of your database file.

1. Stop qui.
2. Make a backup with `sqlite3 /config/qui.db ".backup '/backup/qui.db'"`. The backup also reads the WAL, so it holds each change.
3. Do one of these steps:
   - To shrink the file on its own disk, run `sqlite3 /config/qui.db 'VACUUM;'`. The disk needs free space of up to the size of the file. The temp directory of the system, for example `/tmp`, also needs free space of up to the size of the file.
   - To write a small copy on another disk, run `sqlite3 /config/qui.db "VACUUM INTO '/mnt/other/qui.db';"`. The other disk needs free space for the small copy. Copy the small copy into the data directory as `qui.db.new`. The disk of the database needs free space for this copy. If the copy fails, the old file is still in place. Then move `qui.db` out of the data directory. If `qui.db-wal` or `qui.db-shm` exist, move them out too. Rename `qui.db.new` to `qui.db`, and give it the owner of the old file.
4. Start qui.

Do not leave an old `qui.db-wal` file next to the small copy. SQLite applies it to the new file and damages the database.

When qui runs correctly, you can delete the backup and the old file.

## Low disk

On SQLite, qui reads the free space on the disk of the database file each minute. When the free space is below a limit, qui shows the System notice **Low disk** in the sidebar, in the header menu, and in the mobile menu. The limit is the smaller of 1 GB and 5% of the size of the disk. For example, a disk of 10 GB has a limit of 500 MB, and a disk of 20 GB or more has a limit of 1 GB. The notice closes when the free space recovers to the limit plus 10%, for example 550 MB on a disk of 10 GB.

When the disk is full, each write of qui fails. Before that happens, do one of these steps:

- Free space on the disk.
- Lower [Keep daily detail for](#keep-daily-detail-for).
- [Delete older history](#delete-history).

qui continues to record while the disk is low. The recording writes a few kilobytes each 5 minutes.

If you keep a small disk on purpose, click **Dismiss**. The notice then stays hidden, also after a restart of qui. The notice opens again in two cases:

- The free space recovers to the limit plus 10%, and then it drops below the limit again.
- After the dismiss, a write of the transfer history fails, and the System notice **Recording stopped** opens.

Each time the notice opens, qui sends it one time to each [notification target](./notifications.md) that has the event `system_notice`.

On Postgres, qui does not read the disk, and the notice never opens.

## Why the numbers differ from Server Statistics

Server Statistics on the [Dashboard](./dashboard.md) reads the all-time counters of qBittorrent. These counters also count protocol traffic. The Statistics page adds up the transfer of each torrent. Thus, the two pages show different numbers.

## Incognito mode

If [incognito mode](./incognito.md) is on, the page replaces the torrent names, the tracker names, the category names, and the release groups with fake names. Resolution, source, and content type stay visible.
