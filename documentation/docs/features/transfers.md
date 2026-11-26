---
sidebar_position: 15.6
title: Move torrents and their files to another qBittorrent instance
sidebar_label: Transfers
description: "Transfer to Instance moves a torrent, its cross-seeds, and their files from one qBittorrent instance to another. qui copies the files, checks every piece, adds the torrents on the target, and then removes or keeps them on the source. The Transfers page shows each Transfer."
---

# Transfers

A Transfer moves a torrent and its files from one qBittorrent instance to another instance in qui. For example, you can move a torrent from a seedbox to a home server after 90 days. qui copies the files that the target does not have, checks every piece against the `.torrent`, and adds the torrent on the target. Then qui removes the torrent from the source, or keeps it, as you select.

A Transfer also moves each torrent on the source instance that reads the same files, for example a cross-seed. A cross-seed that reads the files through hardlinks stays on the source in two cases. The first is a [Transfer torrent](#transfer-torrent). The second is a source whose files qui reaches [over SSH](#cross-seeds-on-hardlinks). The source is the instance that has the torrent now. The target is the instance that gets it.

To move only a torrent whose files are already on the target, use [Export to Instance](./automations.md#export-to-instance).

## Requirements

- Transfer needs qBittorrent 5.1 or newer on the source and on the target. qui refuses an older instance with the message `Transfer needs qBittorrent 5.1 or newer. Update qBittorrent on <instance>.` Export to Instance continues to work with older versions.
- The torrent must be complete. qui does not move a torrent with skipped files, or a v2 or hybrid torrent.
- The target must not have the torrent. If the target has it, the Transfer stops with "already on the target".

## Start a Transfer

You can start a Transfer in two ways:

- In the torrent list, right-click one torrent and click **Transfer to instance…**. On a phone, press and hold one torrent to select it. Then tap **More** in the bar at the bottom, and tap **Transfer to instance…**. The dialog shows a summary before the start: the torrents of the group, their size, the way that qui uses, and the free space on the target. If the target has less free space than the Transfer needs, a warning shows next to **Start Transfer**. A warning also shows there if qui cannot see the files on the target.
- In an automation, add the action **Transfer to Instance**. Each torrent that matches the rule starts one Transfer. A [Transfer torrent](#transfer-torrent) has one exception, for cross-seeds on hardlinks. A dry run lists the Transfers that the rule starts in a live run. See [Automations](./automations.md#transfer-to-instance).

The dialog and the action have the same fields:

| Field | Description |
| --- | --- |
| **Target instance** | The instance that gets the torrent. It cannot be the source. |
| **Save path** | The folder on the target. qui keeps the folders of the torrent and its cross-seeds below it. If you leave it empty, qui uses the save path that the target uses for the category. If you also leave the category empty, qui uses the default save path of the target. If you leave the save path empty and Automatic Torrent Management is the default mode of the target, qui turns it on for the torrent. If a cross-seed is in a folder below that save path, each torrent of the Transfer stays on manual management. If the way is **No copy** and the source keeps seeding, the torrent also stays on manual management. The two instances then read the same files, and the target must not move them. The action accepts the [save path templates](./automations.md#save-path-templates) of Export to Instance. |
| **Category** | The category on the target. |
| **Tags** | The tags on the target. |
| **After the Transfer** | What qui does with the torrent on the source when the target seeds it. See [After the Transfer](#after-the-transfer). |
| **Leave the torrent stopped on the target** | qui adds the torrent on the target and does not start it. |
| **Way** | How the files go to the target. See [Ways](#ways). |

qui runs three Transfers at a time for each source instance. The other Transfers of that source wait. If two Transfers write at the same path on the target, they run one after the other, and the second Transfer uses the files of the first. Before a Transfer starts, qui checks the free space on the target, and reserves the space for each running Transfer to the same target.

On qBittorrent 5.1 and 5.2 with no file access to the target, qBittorrent reports the free space only at its default save path. For another save path, the summary shows "Free space unknown".

## Ways

The way is how the files go from the source to the target. The **Way** field has three values:

- **Auto** (default): qui selects the way. The table below shows the selection.
- **Copy through qui**: qui copies the files. qui must reach the files of both instances, on its own disk or over [SSH](./ssh-access.md).
- **Transfer torrent**: the two qBittorrent clients pass the files to each other with a Transfer torrent.

With **Auto**, qui selects the way from the [file access](./instance-settings.md#local-filesystem-access) of each instance:

| Source | Target | Way |
| --- | --- | --- |
| qui's own disk | qui's own disk | **Copy on qui's disk**. qui makes hardlinks when both folders are on one filesystem. |
| qui's own disk or SSH | SSH or qui's own disk | **Copy over SFTP**. qui uses four SSH connections. |
| SSH | SSH | **BitTorrent**, with a copy over SFTP as the fallback. |
| No file access on one side | Any | **BitTorrent**. |

If the two instances see the same files at the same place, no bytes move. The Transfers page shows the way as **No copy**.

### Copy through qui

qui reads the files from the source and writes them on the target. A big file goes in four parts at the same time, and small files go many at a time. If the SSH server refuses an extra connection, qui continues with fewer connections, down to one. Other features that use SSH continue to work.

qui writes each new file under a temporary name, and renames it when the file is complete. qui does not replace a file that is already on the target.

#### Cross-seeds on hardlinks

If the files of the source are on qui's own disk, qui finds the cross-seeds that read the files through hardlinks. They move in the same Transfer. qui copies each file one time and links the other names to that copy on the target. If the target cannot make a hardlink, the file gets a copy of its own.

qui finds hardlinks only on its own disk. If qui reaches the files of the source over SSH, a cross-seed on hardlinks is not part of the Transfer:

- The summary of the dialog and the log of the Transfer do not count it.
- It stays on the source and continues to seed.
- With **Remove the torrent and its files from the source**, qui removes only the files of the moved torrent. The data stays on the source while one of these cross-seeds uses it.

To move such a cross-seed, start a Transfer for it. If its files have the same names and the same save path as files that are already on the target, qui copies nothing. If the names are different, the target gets a full copy of the data for each cross-seed.

### Transfer torrent

A Transfer torrent is a temporary private copy of the torrent, with a new infohash and the same pieces. The trackers of the real torrent do not see it. The source client seeds it from its files, and the target client downloads it. qui removes the Transfer torrent from both clients at the end.

The target connects to the [public peer address](./instance-settings.md#public-peer-address) of the source. If no bytes arrive in 2 minutes, qui asks the source to connect to the target. If that also fails and qui can reach the files of both instances over SSH, qui copies the files over SFTP. Otherwise, the Transfer fails and tries again later.

If an instance uses the default "TCP and μTP" peer connection protocol of qBittorrent, the Transfers page shows a hint. In qui's tests, "TCP only" was about three times faster. qui does not change this setting.

A Transfer torrent cannot make hardlinks on the target. Thus only the torrent that you selected moves, with the torrents that read its files at the same path. A cross-seed that reads the files through hardlinks stays on the source and continues to seed. If the files of the source are on qui's own disk, the summary and the log of the Transfer show how many cross-seeds stay. qui also keeps the files of the moved torrent on the source, because the cross-seeds use the same data. These files use no more disk space.

An automation can match several cross-seeds that have the same files through hardlinks. The rule then starts a Transfer for one of them. It skips the others while that Transfer is not complete, and while the target has one of them. A file such as an .nfo or a subtitle does not make two cross-seeds different. A cross-seed that has one more video file, for example a longer season pack, gets a Transfer of its own. The activity log of the rule does not show the torrents that the rule skips.

With **Keep seeding on the source**, the files go to the target one time. With the other two values of **After the Transfer**, the moved torrent leaves the source. The next run of the rule then moves the next of these cross-seeds.

To move these cross-seeds too, give qui file access to the target, on qui's own disk or over SSH. You can also cross-seed again on the target.

## The proof

qui does not remove anything from the source before it proves the copy on the target:

1. While qui copies, it checks each piece against the hash in the `.torrent`. A bad byte from the source stops the Transfer.
2. qui asks the target to confirm that the bytes are on its disk. A piece that passed the check and that the target confirmed is a proven piece.
3. If the SFTP server of the target cannot confirm writes, qui adds a temporary torrent on the target that only checks the files. The check must pass for every piece.
4. qui adds the real torrent on the target with "Skip hash check", stopped. The private tracker never sees a download.
5. qui stops the torrent on the source, and then starts the torrent on the target.

With a Transfer torrent, qBittorrent on the target checks each piece that it downloads.

If you renamed a file of the torrent on the source, qui gives the file the same name on the target. qBittorrent on the target then checks the files again. The Transfer continues only when every piece passes.

Files that are already on the target, for example from rclone, are a head start. qui checks them and copies only the files that are missing. If a file on the target has the same name but other content, the Transfer stops and names the file. qui does not overwrite it, because another torrent or your library can use it. qui judges a file only by the pieces that are fully inside the file.

If qui cannot see the files on the target, the summary warns that a file with other content at the same path can be replaced.

## Resume after a stop

A Transfer continues after a restart of qui or after a lost connection. The Transfer keeps its proven pieces, and qui copies only the pieces that are not proven. The progress on the Transfers page continues from the proven pieces, so it can show a lower value than before the stop. qui copies again each piece that the target did not confirm before the stop.

If the target cannot confirm writes, qui first checks the partial files on the target, and then copies the pieces that failed. qui never starts a file again from its beginning.

## After the Transfer

The field **After the Transfer** has three values:

- **Remove the torrent and its files from the source** (default).
- **Remove the torrent from the source and keep its files**.
- **Keep seeding on the source**. The torrent seeds on both instances.

With the first two values, qui stops the torrent on the source before it starts the torrent on the target. Thus, the private tracker does not see two seeding locations at the same time.

Seeding time and ratio start at zero on the target.

## Files that stay on the source

With **Remove the torrent and its files from the source**, qui removes the torrent from the source. qui keeps some files on the source, and the Transfer says why and how much space stays in use. qui keeps the files in these cases:

- The target reads the same files, and the way is **No copy**.
- qui did not copy the file in this Transfer, for example a file that was already on the target. The file on the target can be the same file as on the source, for example on a NAS that both instances mount. A delete would then remove your only copy. qui removes such a file only when both instances are on qui's own disk and qui can prove that two separate copies exist.
- Another torrent reads the file, for example a cross-seed on another instance. qui uses the same check as the delete mode `deleteWithFilesPreserveCrossSeeds`.
- Another instance in qui uses the same SSH server as the source, with the same host and port or the same host key. The other instance can use the files.
- qui cannot reach the files of the source to remove only the files that it copied.
- With a Transfer torrent, fewer bytes arrived on the target than the files have. The target can then read some files from the disk of the source.

Cross-seeds on a [Cross-seed instance](./cross-seed/overview.md) at the source stay, and their files also stay.

If the target has a Cross-seed instance, the cross-seeds of a group go to that Cross-seed instance on links. The main torrent goes to the target. A group with different files, such as a season pack and one of its episodes, goes whole to the target.

## While a Transfer runs

- The torrent on the source continues to seed during the copy.
- qui and its automations refuse to delete, move, or rename a torrent that a Transfer holds.
- If the source torrent changes during the copy, for example a move or a rename, the Transfer fails. If qBittorrent checks a torrent of the Transfer again, for example with **Recheck torrents on completion**, the Transfer waits for the check to end. It fails when the check of the source finds missing pieces. A check that runs for more than an hour is a temporary failure.
- In one automation run, a Delete action wins over a Transfer. A Move action in the same run makes the Transfer wait.
- If an instance is off, its Transfers wait. If you delete an instance, its Transfers fail.
- If you change the URL, the SSH user, the host key, or the file access of an instance, qui stops its Transfers. You decide what happens next.

A temporary failure, for example a lost connection, tries again 1 hour later or after. A permanent failure, for example a conflict or a format that qui does not support, waits for you.

Copied files get the permissions 775 for folders and 664 for files where the target allows it. Thus, qBittorrent can delete them later.

## The Transfers page

To open the page, click **Transfers** in the sidebar. On a phone, tap **Settings** in the bottom bar, and then tap **Transfers**. The number beside **Transfers** in the sidebar counts the Transfers that run or wait for their turn, and the Shares that run.

The **Transfers** tab has one row for each Transfer and for each run of an Export to Instance rule. A row shows the torrents, the size of the torrent, the source and the target, the way, the progress, the speed, the time left, the state, and who started it. On a phone, a row does not show the way. **Details** shows it. The chips **Active**, **Waiting**, **Finished**, and **Failed** filter the rows. The list shows 50 rows on each page. To see the other rows, use **Previous** and **Next** below the list.

The states are **Waiting**, **Copying**, **Checking**, **Finishing**, **Done**, **Failed**, and **Cancelled**.

Each row has these actions:

- **Details** opens a dialog with four tabs: an overview with the reason of a failure, the torrents, the steps, and the log.
- **Retry** starts a failed or cancelled Transfer again.
- **Cancel** stops the Transfer. qui removes the files that it wrote and the torrents that it added on the target, and starts the torrent on the source again. After the removal from the source starts, a cancel does not roll back.
- **Remove** removes the row of a Transfer that ended.

On a phone, the actions of a row are in one menu.

To act on many rows at once, select the box at the start of each row. To select a range of rows, hold Shift when you select the box of the last row. The box in the header selects all the rows of the page. After you select all the rows of the page, **Select all** selects each row of the chip, also the rows on the other pages. When you confirm an action after **Select all**, qui acts on the rows that are in the chip at that time. These can include rows that an automation added after the selection.

A bar at the bottom of the list shows **Retry**, **Cancel**, and **Remove** for the selected rows. An action changes only the rows where it is possible, and qui skips the other rows. For example, **Cancel** skips a Transfer that ended. **Retry** acts only on rows of the page that you select. After **Select all**, the bar does not show **Retry**, because qui plans each Transfer again. qui asks before it cancels or removes rows. The message after the action tells you how many rows changed and how many qui skipped. If qui refused a row, the message gives the reason.

Finished rows stay for 30 days. The row of a permanent failure stays longer, because it stops an automation from a new start of the same Transfer. qui removes that row after 30 days only if a later Transfer of the same torrents exists. To let the automation start the Transfer again, remove the row.

The notification events **Transfer failed** and **Transfer finished** tell you about the result. See [Notifications](./notifications.md).

## Helper torrents

A helper torrent is a torrent that qui adds for a Transfer or a [Share](./shares.md). The Transfer torrent is a helper torrent. A torrent that only checks files on the target is also a helper torrent.

- A helper torrent is private. It has no passkey, comment, web seed, "created by" line, or source tag.
- The torrent list shows a badge on a helper torrent. qui refuses "delete with files" on it.
- qBittorrent's share limits do not apply to a helper torrent.
- Automations, cross-seed, Dir Scan, orphan scan, Statistics, notifications, backups, reannounce, and External Programs ignore helper torrents. Orphan scan does not touch the folders of a running Transfer.

qBittorrent's own "Run external program on torrent added" and "Run external program on torrent finished" still run for helper torrents. qui adds each helper torrent of a Transfer with the tag `qui-transfer`, and each Share torrent with the tag `qui-share`. A script can read the tags with `%G` and skip these torrents.
