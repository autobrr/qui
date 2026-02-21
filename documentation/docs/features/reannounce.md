---
sidebar_position: 3
title: Reannounce
description: Automatically fix stalled torrents by reannouncing to trackers.
---

# Tracker Reannounce

qui can automatically fix stalled torrents by reannouncing them to trackers. This helps when a tracker fails to register a new upload immediately, ensuring your torrents start seeding without manual intervention.

qBittorrent doesn't retry failed announces quickly. When a tracker is slow to register a new upload or returns an error, you may be stuck waiting for a long time. qui handles this automatically and gracefully.

qui never spams trackers. While a tracker is still updating or waiting for a response, qui waits patiently. It only acts once a tracker has responded and there's an actual problem to fix.

## Quick Start

1. Go to **Services** in the main navigation.
2. Select an instance from the dropdown.
3. In the **Tracker Reannounce** section, toggle **Enabled** to turn it on.
4. Click **Save Changes**.

That's it! qui will now monitor stalled torrents in the background.

## Configuration

### Timing

| Setting | Description | Default |
|---------|-------------|---------|
| Initial Wait | How long to wait after a torrent is added before checking it | 15s |
| Retry Interval | How often to retry within a single reannounce attempt | 7s |
| Max Torrent Age | Stop monitoring torrents older than this | 10 mins |
| Max Retries | Maximum consecutive retries within a single scan cycle | 50 |

Some slow trackers need up to 50 retries at 7s intervals (~6 minutes) to register uploads.

### Monitoring Scope

You can choose which torrents to monitor:

- **Monitor All Stalled Torrents**: Checks every stalled torrent.
  - Use **Exclusions** below to ignore specific Categories, Tags, or Trackers (e.g., ignore "public" trackers).
- **Custom Filter (Monitor All Disabled)**:
  - Only checks torrents that match your **Include** rules.
  - You can still add **Exclusions** to block specific items within those allowed groups.

### Quick Retry

By default, qui waits about **2 minutes** between reannounce attempts for the same torrent (a per-torrent cooldown between scans).

- **Enable Quick Retry** to use the **Retry Interval** (default 7s) as the cooldown instead. This helps stalled torrents recover faster.
- The **Retry Interval** controls both the spacing of retries inside each scan attempt and, with Quick Retry enabled, the cooldown between scans.

This is especially useful on trackers that are slow to register new uploads. Some sites take a moment before they recognize a new torrent, which can cause initial stalls—Quick Retry helps work around this automatically.

## Activity Log

To see what's happening:

1. Go to **Services** and select your instance.
2. Click the **Activity Log** tab in the Tracker Reannounce section.

You will see a real-time feed of every torrent checked, whether the reannounce succeeded, failed, or was skipped (e.g., because the tracker is actually working fine).
