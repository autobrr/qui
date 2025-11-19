# Tracker Reannounce Monitoring in qui

qui can proactively reannounce torrents whose trackers initially respond with “unregistered” or transient outage errors, using a small, conservative retry loop. This feature keeps brand‑new uploads healthy without spamming trackers.

## Overview

1. **Per‑instance opt in** – enable monitoring individually on each qBittorrent instance.
2. **Scoped monitoring** – watch all torrents or only specific categories, tags, or tracker domains.
3. **Configurable timings** – control the initial wait (to let qBittorrent finish its first announce) and the reannounce interval.
4. **Background engine** – qui scans newly added torrents, watches tracker status, and issues reannounce calls with conservative retry limits.
5. **Proxy interception** – `/api/v2/torrents/reannounce` calls for instances with monitoring enabled are debounced so external scripts (qbrr, etc.) do not flood trackers.

## Enabling the feature

1. Navigate to **Dashboard** and locate the instance card.
2. Click the **cogwheel** to open the **Instance Preferences** modal.
3. Switch to the **Reannounce** tab and flip the toggle on.
4. Adjust the inputs to match your tracker’s etiquette:
   - **Initial tracker wait** – seconds to wait for the first announce before reannounce attempts (default 15s).
   - **Reannounce interval** – delay between retries (default 7s, matching qBittorrent’s built‑in value).
   - **Monitor torrents added within** – age cutoff based on qBittorrent’s “active time”; torrents whose active time exceeds this are ignored (default 600s / 10 minutes).
5. Choose your **Monitor scope**:
   - **Monitor all**: Enables monitoring for all torrents, subject to any explicit exclusions you define.
   - **Monitor specific...** (Monitor all OFF): Enables "Allowlist" mode. You must specify at least one Category, Tag, or Tracker domain to include. Only matching torrents will be monitored.
6. **Configure Exclusions** (Optional):
   - You can explicitly **Exclude** specific Categories, Tags, or Tracker domains.
   - **Exclusions take priority**: If a torrent matches an exclusion, it will be ignored regardless of "Monitor all" or inclusion settings.
   - Useful for filtering out public trackers, specific labels, or categories that don't need monitoring.
7. Save the instance. Settings are remembered per instance.

## What happens once it’s enabled?

- qui watches the instance’s sync data for torrents whose trackers report:
  - “Unregistered”‑style messages (matched against built‑in patterns).
  - Known outage phrases (“tracker is down”, “maintenance”, etc.).
- Only torrents that match your scope (and are not excluded), have a detected tracker problem, are still within the configured max active‑time window, and have no working trackers will be considered for reannounce.
- For problematic torrents whose trackers are still in an “updating / not contacted yet” state, qui waits up to the configured **Initial tracker wait** to let qBittorrent finish its first announce cycle. If the tracker becomes healthy during this window, no reannounce is issued.
- For each eligible torrent, qui runs a background job that:
  - Uses qBittorrent’s `/torrents/reannounce` API via `ReannounceTorrentWithRetry`.
  - Spaces attempts by your configured **Reannounce interval**.
  - Stops as soon as any tracker reaches an OK state without an “unregistered”‑style message.
  - Gives up after a small fixed budget of attempts (currently 3) if trackers never become healthy.

## Proxy interception & debouncing

When monitoring is enabled for an instance:

- qui intercepts `/api/v2/torrents/reannounce` requests for that instance (including requests made by external clients through the proxy). For instances where monitoring is disabled, these requests are forwarded directly to qBittorrent unchanged.
- For each intercepted request, qui looks up the hashes in its sync data. Hashes that both fall within your monitoring scope and currently have problematic trackers are **not** forwarded directly to qBittorrent; they are queued in the internal reannounce worker instead. Other hashes in the same request are forwarded upstream as usual.
- Duplicate calls for the same hash while a job is running, or within a short per‑hash cooldown window, are recorded as skipped and not re‑scheduled (debounced).
- Responses stay consistent (`200 OK`) so automation tools do not need changes; fully handled requests return an `Ok.` body, and mixed / unhandled hashes still receive qBittorrent’s normal response.

## UI indicators

- The Instance list shows whether tracker monitoring is enabled.
- The Instance form carries all monitoring settings for easy adjustments.

## Tips & best practices

- Start with the defaults: they mirror qbrr’s conservative timing and work well for most trackers.
- If your tracker takes longer to register uploads, raise the **initial tracker wait** (e.g. 30–60 seconds).
- Keep the **monitor torrents added within** window narrow (≤15 minutes) so older torrents are not reannounced unnecessarily.
- Use **Exclusions** to filter out noise. For example, exclude `public` tag or specific tracker domains that are known to be flaky or don't support reannounce.
- Use the scope filters to avoid tracking torrents that do not need this feature (e.g., freeleech categories or private trackers that already auto reannounce on their end).
- Remember that the reannounce monitor never deletes torrents if reannounce fails; it simply logs the failure and moves on.

For any issues, check the server logs (`reannounce:` entries) to see how the tracker monitoring service is behaving per instance/hash.
