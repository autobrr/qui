# Tracker Reannounce

qui can automatically fix stalled torrents by reannouncing them to trackers. This helps when a tracker fails to register a new upload immediately, ensuring your torrents start seeding without manual intervention.

## Quick Start

1. Go to **Settings > Instances** (or click the cogwheel on an instance card).
2. Open the **Tracker Reannounce** tab.
3. Toggle **Enabled** to turn it on.
4. Click **Save Changes**.

That’s it! qui will now monitor stalled torrents in the background.

## Configuration

### Timing
* **Initial Wait**: How long to wait after a torrent is added before checking it (default: 15s). This gives the tracker time to work normally before we interfere.
* **Retry Interval**: How often to retry if the tracker is still reporting errors (default: 7s).
* **Max Torrent Age**: Stop monitoring torrents older than this (default: 10 mins). Prevents checking old, permanently dead torrents.

### Monitoring Scope
You can choose which torrents to monitor:

* **Monitor All Stalled Torrents (Default)**: Checks every stalled torrent.
  * Use **Exclusions** below to ignore specific Categories, Tags, or Trackers (e.g., ignore "public" trackers).
* **Custom Filter (Monitor All Disabled)**:
  * Only checks torrents that match your **Include** rules.
  * You can still add **Exclusions** to block specific items within those allowed groups.

### Aggressive Mode
By default, qui waits about **2 minutes** between reannounce attempts for the same torrent to be polite to trackers.
*   **Enable Aggressive Mode** to remove this cooldown and retry immediately on the next scan (every 7s) if the torrent is still stalled.

## Activity Log
To see what’s happening:
1. Go to the **Tracker Reannounce** tab.
2. Click **Activity Log**.

You will see a real-time feed of every torrent checked, whether the reannounce succeeded, failed, or was skipped (e.g., because the tracker is actually working fine).
