---
sidebar_position: 6
title: Logging
description: Log levels, rotation, the live log viewer, and log exclusions.
---

# Logging

The **Logs** tab under **Settings** shows a live view of the qui log stream. You can also change the logging configuration without a restart.

## Configuration

Click the gear button in the top right corner of the Logs panel to open the configuration form. Click **Save Settings** to apply your changes.

| Field | Description | Default |
|-------|-------------|---------|
| Log Level | One of `TRACE`, `DEBUG`, `INFO`, `WARN`, `ERROR` | `DEBUG` |
| Log File Path | File to write logs to. Leave empty to log to stdout only. | empty |
| Max Size (MB) | File size that starts a rotation. Minimum 1. | 50 |
| Max Backups | Number of rotated files that qui keeps. `0` keeps all. | 10 |

qui writes these values to `config.toml` and applies them immediately. The same keys are also available as `config.toml` entries and environment variables. See [Configuration Reference](./reference.md) and [Environment Variables](./environment.md) for the full tables.

:::note
If an environment variable such as `QUI__LOG_LEVEL` sets a value, the matching field shows a lock badge and you cannot change it from the UI. Change or remove the environment variable instead. The `--log-path` flag of `qui serve` also locks the **Log File Path** field. Remove the flag to edit the path in the UI.
:::

### Which level to use

`DEBUG` is the default and records enough detail for most problem reports. When you report an issue, use `DEBUG` logs. `TRACE` adds per-request and per-sync-tick detail and makes the log grow quickly. If a developer asks for it, enable `TRACE`.

## Live log viewer

The viewer connects to the qui log stream over SSE. On connect, it loads the most recent 1,000 lines and then follows new output live. A green dot shows a healthy connection. If the connection drops, the viewer reconnects after 3 seconds.

The toolbar gives you these controls:

- **Search**. Matches against the log message and the structured fields of each entry.
- **Level filter**. Select which of the five levels to show. **All** and **None** toggle every level at once.
- **Clear**. Empties the current view.
- **Auto-scroll**. Follows the newest entries. Turn it off to pause and scroll back.

While auto-scroll is on, the viewer keeps the newest 1,000 entries and trims older ones. When auto-scroll is off, it keeps up to 10,000 entries. When that limit is reached, qui drops the oldest entries and shows a warning.

Click a structured entry to open it in a dialog with formatted JSON. **Copy Raw** copies the original log line. **Copy JSON** copies the formatted version.

## Muting messages

Hover over the level badge of a noisy entry and click the **X** to mute it. The viewer then hides every entry with the same message text. Structured fields can differ, so one mute covers a repeating message across all instances.

When at least one mute exists, a button with the mute count, for example **3 Muted**, appears in the toolbar. Open it to unmute single messages or click **Clear all** to remove every mute. qui stores the mute list in its database, so it applies in every browser you use.

## Log files

When you set a log file path, a **Log files** list appears below the viewer. It shows the active log file and its rotated backups with size and modification time. Click the download button to save a file to your machine.

qui compresses rotated files with gzip. The download endpoint serves only qui's own log files from the log directory, not other files that live next to them.
