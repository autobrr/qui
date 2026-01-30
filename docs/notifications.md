# Notifications

qui uses Shoutrrr to deliver notifications. Configure one or more targets in **Settings → Notifications** and choose which events to send.

## Setup

1. Open **Settings → Notifications**.
2. Add a target name and Shoutrrr URL.
3. Pick the events you want.
4. Save and use **Test** to verify delivery.

Notes:
- Existing targets keep their saved event list when new events are introduced.
- Messages may be truncated to keep notifications short and avoid provider limits.
- Discord and Notifiarr targets use rich embeds with fields; other services receive plain text.

## Event types

| Event key | Description |
| --- | --- |
| `torrent_completed` | A torrent finishes downloading (includes tracker, category, and tags when available). |
| `backup_succeeded` | A backup run completes successfully. |
| `backup_failed` | A backup run fails. |
| `dir_scan_completed` | A directory scan run finishes. |
| `dir_scan_failed` | A directory scan run fails. |
| `orphan_scan_completed` | An orphan scan run completes (including clean runs). |
| `orphan_scan_failed` | An orphan scan run fails. |
| `cross_seed_automation_succeeded` | RSS cross-seed automation completes (summary counts and samples). |
| `cross_seed_automation_failed` | RSS cross-seed automation fails or completes with errors (summary). |
| `cross_seed_search_succeeded` | Seeded search run completes (summary counts and samples). |
| `cross_seed_search_failed` | Seeded search run fails or is canceled (summary). |
| `automations_actions_applied` | Automation rules applied actions (summary counts and samples; only when actions occur). |
| `automations_run_failed` | Automation rules failed to run for an instance (system error). |

## Shoutrrr URLs

Use any Shoutrrr-supported URL scheme. A few examples:

- `discord://token@channel`
- `notifiarr://apikey`
- `slack://token@channel`
- `telegram://token@chat-id`
- `gotify://host/token`

Notifiarr can also include optional parameters such as `channel` or `name`, e.g. `notifiarr://apikey?name=qui&channel=123456789`.

See Shoutrrr documentation for the full list of services and URL formats.
