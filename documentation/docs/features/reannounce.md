---
sidebar_position: 4
title: Reannounce
description: Automatically fix stalled torrents by reannouncing to trackers.
---

# Tracker Reannounce

qui reannounces stalled torrents to trackers. If a tracker fails to register a new upload immediately, qui helps your torrents start seeding without manual work.

qBittorrent does not retry failed announces quickly. If a tracker registers a new upload slowly or returns an error, the torrent stays stalled. qui retries the announce for you.

qui never spams trackers. If a tracker update is in progress or a response is pending, qui waits. qui acts only after the tracker responds and a problem exists.

## Quick start

1. Open **Automations** in the main navigation.
2. Find your instance in the **Reannounce** card.
3. Turn on the switch next to the instance name. In the dialog that opens, click **Enable**.
4. If you want to change settings, expand the instance row and click **Configure**. Adjust the values and click **Save Changes**.

qui now monitors stalled torrents in the background.

## Configuration

### Timing

| Setting | Description | Default |
|---------|-------------|---------|
| Initial Wait | Time to wait after you add a torrent before qui checks it | 15s |
| Retry Interval | Time between retries within a single reannounce attempt | 7s |
| Max Torrent Age | Stop monitoring torrents older than this | 10 minutes |
| Max Retries | Maximum consecutive retries within a single scan cycle | 50 |

Some slow trackers need up to 50 retries at 7s intervals (about 6 minutes) to register uploads.

### Monitoring scope

Choose which torrents qui monitors:

- **Monitor All Stalled Torrents**: qui checks every stalled torrent. If you want to ignore specific categories, tags, or trackers (such as public trackers), add **Exclude** rules.
- **Custom filter (Monitor All disabled)**: qui checks only torrents that match your **Include** rules. **Exclude** rules still block specific items within those groups.

### Quick Retry

By default, qui waits about **2 minutes** between reannounce attempts for the same torrent. This duration acts as a per-torrent cooldown between scans.

If you enable **Quick Retry**, qui uses the **Retry Interval** (default 7s) as the cooldown instead. Stalled torrents then recover faster. The **Retry Interval** controls the spacing of retries inside each scan attempt. If you enable **Quick Retry**, it also controls the cooldown between scans.

Quick Retry helps on trackers that register new uploads slowly. Some sites need time before they recognize a new torrent, which causes initial stalls.

## Activity log

If you want to view activity:

1. Open **Automations** and find your instance in the **Reannounce** card.
2. Expand the instance row, click **Configure**, then select the **Activity Log** tab.

The log displays a real-time feed of every checked torrent. It shows whether qui succeeded, failed, or skipped the reannounce, for example because the tracker already works.
