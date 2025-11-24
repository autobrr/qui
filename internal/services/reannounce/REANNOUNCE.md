# Tracker Reannounce

qui can automatically fix stalled torrents by reannouncing them to trackers. This helps when a tracker fails to register a new upload immediately, ensuring your torrents start seeding without manual intervention.

qBittorrent doesn't retry failed announces quickly. When a tracker is slow to register a new upload or returns an error, you may be stuck waiting for a long time ([related issue](https://github.com/qbittorrent/qBittorrent/issues/11419)). qui handles this automatically and gracefully.

qui never spams trackers. While a tracker is still updating or waiting for a response, qui waits patiently. It only acts once a tracker has responded and there's an actual problem to fix.

## Quick Start

1. Go to **Settings > Instances** (or click the cogwheel on an instance card).
2. Open the **Tracker Reannounce** tab.
3. Toggle **Enabled** to turn it on.
4. Click **Save Changes**.

That’s it! qui will now monitor stalled torrents in the background.

## Configuration

### Timing
* **Initial Wait**: How long to wait after a torrent is added before checking it (default: 15s). This gives the tracker time to work normally before we interfere.
* **Retry Interval**: How often to retry within a single reannounce attempt (up to 3 tries; default: 7s). This is separate from the scan cooldown below.
* **Max Torrent Age**: Stop monitoring torrents older than this (default: 10 mins). Prevents checking old, permanently dead torrents.

### Monitoring Scope
You can choose which torrents to monitor:

* **Monitor All Stalled Torrents (Default)**: Checks every stalled torrent.
  * Use **Exclusions** below to ignore specific Categories, Tags, or Trackers (e.g., ignore "public" trackers).
* **Custom Filter (Monitor All Disabled)**:
  * Only checks torrents that match your **Include** rules.
  * You can still add **Exclusions** to block specific items within those allowed groups.

### Quick Retry
By default, qui waits about **2 minutes** between reannounce attempts for the same torrent (a per-torrent cooldown between scans).
*   **Enable Quick Retry** to use the **Retry Interval** (default 7s) as the cooldown instead. This helps stalled torrents recover faster.
*   The **Retry Interval** controls both the spacing of the up-to-3 retries inside each scan attempt and, with Quick Retry enabled, the cooldown between scans.

This is especially useful on trackers that are slow to register new uploads. Some sites take a moment before they recognize a new torrent, which can cause initial stalls—Quick Retry helps work around this automatically.

## Activity Log
To see what’s happening:
1. Go to the **Tracker Reannounce** tab.
2. Click **Activity Log**.

You will see a real-time feed of every torrent checked, whether the reannounce succeeded, failed, or was skipped (e.g., because the tracker is actually working fine).
