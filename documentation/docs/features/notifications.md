---
sidebar_position: 7
title: Notifications
description: Send events to Shoutrrr targets and Notifiarr.
---

# Notifications

qui supports both the Notifiarr API and Shoutrrr targets. Configure one or more targets in **Settings → Notifications** and choose which events to send.

## Setup

1. Open **Settings → Notifications**.
2. Add a target name and URL.
3. Pick the events you want.
4. Save and use **Test** to verify delivery.

Notes:
- Existing targets keep their saved event list when new events are introduced.
- Messages may be truncated to keep notifications short and avoid provider limits.
- Discord and Notifiarr targets use rich embeds with fields; other services receive plain text.

## Event types

| Event key | Description |
| --- | --- |
| `torrent_added` | A torrent is added (includes tracker, category, tags, and ETA when available). |
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
| `cross_seed_completion_succeeded` | Completion search run completes (summary counts and samples). |
| `cross_seed_completion_failed` | Completion search run fails. |
| `cross_seed_webhook_succeeded` | Webhook check run completes (summary counts and samples). |
| `cross_seed_webhook_failed` | Webhook check run fails. |
| `automations_actions_applied` | Automation rules applied actions (summary counts and samples; only when actions occur). |
| `automations_run_failed` | Automation rules failed to run for an instance (system error). |

## Notifiarr API

For prettier output similar to Discord embeds, use the native Notifiarr API scheme:

- `notifiarrapi://apikey`
- Optional override: `notifiarrapi://apikey?endpoint=https://notifiarr.com/api/v1/notification/qui`

## Shoutrrr URLs

Use any Shoutrrr-supported URL scheme. A few examples:

- `discord://token@channel`
- `notifiarr://apikey`
- `slack://token@channel`
- `telegram://token@chat-id`
- `gotify://host/token`

Notifiarr can also include optional parameters such as `channel` or `name`, e.g. `notifiarr://apikey?name=qui&channel=123456789`.

See the Shoutrrr documentation for the full list of services and URL formats:
https://github.com/nicholas-fedor/shoutrrr
