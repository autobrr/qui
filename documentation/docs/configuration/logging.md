---
sidebar_position: 6
title: qui log level, log files, and live log viewer
sidebar_label: Logging
description: Set the log level, rotate log files, watch the live log viewer, and mute noisy messages in the live log viewer.
---

# Logging

The **Logs** tab under **Settings** shows a live view of the qui log stream. You can also change the logging configuration without a restart.

## Configuration

The **Log settings** section is below the live log. Change the fields, then click **Save settings** to apply your changes.

| Field | Description | Default |
|-------|-------------|---------|
| Log level | One of `TRACE`, `DEBUG`, `INFO`, `WARN`, `ERROR` | `DEBUG` |
| Log file path | File to write logs to. Leave empty to log to stdout only. A new path must end in `.log`. | empty |
| Max size (MB) | File size that starts a rotation. Minimum 1. | 50 |
| Max backups | Number of rotated files that qui keeps. `0` keeps all. | 10 |

qui writes these values to `config.toml` and applies them immediately. The same keys are also available as `config.toml` entries and environment variables. See [Configuration Reference](./reference.md) and [Environment Variables](./environment.md) for the full tables.

An API key with `Logs: write` changes the other fields, not the log file path. See [Browser-only routes](../api/permissions.md#browser-only-routes). The `.log` rule applies only to a new path set here. A path that is already set stays, and qui uses `logPath` from `config.toml`, `QUI__LOG_PATH`, and `--log-path` as they are.

:::note
If an environment variable such as `QUI__LOG_LEVEL` sets a value, the matching field shows a lock and **Set by an environment variable**, and you cannot change it from the UI. Change or remove the environment variable instead. The `--log-path` flag of `qui serve` also locks the **Log file path** field. Remove the flag to edit the path in the UI.
:::

### Which level to use

`DEBUG` is the default and records enough detail for most problem reports. When you report an issue, use `DEBUG` logs. `TRACE` adds per-request and per-sync-tick detail and makes the log grow quickly. If a developer asks for it, enable `TRACE`.

## Live log viewer

The **Live log** section connects to the qui log stream over SSE. On connect, it loads the most recent 1,000 lines and then follows new output live. Below the log, a green dot and **Connected** show a healthy connection. If the connection drops, the viewer reconnects after 3 seconds.

The header of the section has two buttons:

- **Pause** stops the viewer from scrolling to new entries, so you can scroll back. The button then reads **Follow**. Click it to follow the newest entries again.
- **Clear** empties the current view.

Above the log are two more controls:

- **Search** matches against the log message and the structured fields of each entry.
- The level filter selects which of the five levels to show. The button reads **All levels** when every level shows. **All** and **None** toggle every level at once. qui remembers your choice in every browser you use. The filter only hides entries in the viewer. The **Log level** setting still decides what qui writes to stdout and the log file, and the viewer cannot show levels below it.

While the viewer follows, it keeps the newest 1,000 entries and trims older ones. While it is paused, it keeps up to 10,000 entries. When that limit is reached, qui drops the oldest entries and shows a warning.

Click a structured entry to open it in a dialog with formatted JSON. **Copy raw** copies the original log line. **Copy JSON** copies the formatted version. **Mute similar** mutes the message of the entry, as described below.

## Muting messages

Hover over the level badge of a noisy entry and click the **X** to mute it. On a phone, open a structured entry and tap **Mute similar**. The viewer then hides every entry with the same message text. Structured fields can differ, so one mute covers a repeating message across all instances.

When at least one mute exists, the level filter button shows the mute count, for example **All levels · 3 muted**. Open it to unmute single messages, or click **Clear all** to remove every mute. qui stores the mute list in its database, so it applies in every browser you use.

## Log files

The **Log files** section at the bottom of the tab lists the active log file and its rotated backups with size and modification time. Click **Download** to save a file to your machine. If you did not set a log file path, the section shows **No log files**.

qui compresses rotated files with gzip. The download endpoint serves only qui's own log files from the log directory, not other files that live next to them.
