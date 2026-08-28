---
sidebar_position: 9
title: Notifications
description: Send events to Shoutrrr targets and Notifiarr.
---

# Notifications

qui sends notifications to the Notifiarr API and to Shoutrrr targets. Configure one or more targets in **Settings → Notifications** and select the events to send.

## Setup

1. Open **Settings → Notifications**.
2. Add a target name and URL.
3. Select the events to send.
4. Save, then click the send icon (paper plane) on the target card to send a test notification.

Notes:

- If a qui update adds new events, existing targets keep their saved event list.
- qui truncates long messages to stay within provider limits.
- Discord and Notifiarr targets receive rich embeds with fields. Other services receive plain text.

## Event types

| Event key | Description |
| --- | --- |
| `torrent_added` | The client adds a torrent (includes tracker, category, tags, and ETA when available). |
| `torrent_completed` | A torrent download completes (includes tracker, category, and tags when available). |
| `backup_succeeded` | A backup run completes successfully. |
| `backup_failed` | A backup run fails. |
| `dir_scan_completed` | A directory scan run finishes. |
| `dir_scan_failed` | A directory scan run fails. |
| `orphan_scan_completed` | An orphan scan run completes (includes clean runs). |
| `orphan_scan_failed` | An orphan scan run fails. |
| `cross_seed_automation_succeeded` | RSS cross-seed automation completes (summary counts and samples). |
| `cross_seed_automation_failed` | RSS cross-seed automation fails or completes with errors (summary). |
| `cross_seed_search_succeeded` | Seeded search run completes (summary counts and samples). |
| `cross_seed_search_failed` | Seeded search run fails or cancels (summary). |
| `cross_seed_completion_succeeded` | Completion search run completes (summary counts and samples). |
| `cross_seed_completion_failed` | Completion search run fails. |
| `cross_seed_webhook_succeeded` | Webhook apply adds one or more torrents. No-op checks and applies do not notify. |
| `cross_seed_webhook_failed` | Webhook check or apply fails. |
| `automations_actions_applied` | Automation rules apply actions (summary counts and samples, only if actions occur). |
| `automations_run_failed` | Automation rules fail to run for an instance (system error). |

## Notifiarr API

If you want output similar to Discord embeds, use the native Notifiarr API scheme:

- `notifiarrapi://apikey`
- Optional override: `notifiarrapi://apikey?endpoint=https://notifiarr.com/api/v1/notification/qui`

## Shoutrrr URLs

Use any Shoutrrr-supported URL scheme. Examples:

- `discord://token@channel`
- `notifiarr://apikey`
- `slack://token@channel`
- `telegram://token@telegram?chats=chat-id`
- `gotify://host/token`

Notifiarr also accepts optional parameters such as `channel` and `name`, for example `notifiarr://apikey?name=qui&channel=123456789`.

See the Shoutrrr documentation for the full list of services and URL formats:
https://github.com/nicholas-fedor/shoutrrr
