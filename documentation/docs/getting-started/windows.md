---
sidebar_position: 4
title: Windows
description: Install and run qui on Windows as a background service.
---

# Windows installation

This guide explains how to install qui and create a Windows scheduled task. The task runs qui in the background without an open command prompt window.

## Download

1. Download the latest Windows release from [GitHub Releases](https://github.com/autobrr/qui/releases/latest).
   - For most systems, download `qui_x.x.x_windows_x86_64.zip`.
2. Extract the archive and place `qui.exe` in a directory, for example `C:\qui`.

:::tip
Do not place qui in `C:\Program Files`. The built-in updater writes the new executable next to the old one, and that location needs administrator rights.
:::

## Initial setup

1. Open **Command Prompt** or **PowerShell** and change to the directory:
   ```powershell
   cd C:\qui
   ```

2. Start qui for the first time to generate the default configuration:
   ```powershell
   .\qui.exe serve
   ```

3. Open your browser at [http://localhost:7476](http://localhost:7476) and create your account.

4. If qui works, stop the process with `Ctrl+C`. The next section configures qui as a background task.

### Configuration

qui stores its configuration and runtime data in `%APPDATA%\qui\` by default. With the default SQLite engine, qui stores `qui.db` there too. For more details, see the [Configuration](../configuration/environment.md) section.

## Create a Windows task

Run qui in the background with **Task Scheduler**.

1. Press the **Windows key** and search for **Task Scheduler**.
2. Click **Create Basic Task** in the right sidebar.
3. **Name:** `qui`. Optionally add a description, for example: *qui torrent management service*.
4. **Trigger:** Select **When the computer starts**.
5. **Action:** Select **Start a Program**.
   - **Program/script:** Browse to `C:\qui\qui.exe`
   - **Add arguments:** `serve`
   - **Start in:** `C:\qui`
6. Check **Open the Properties dialog**, then click **Finish**.

### Configure the task properties

In the Properties dialog:

- Under **General**, select **Run whether user is logged on or not**.
- Enter your Windows password when Windows prompts for it.
- If you encounter permission issues, check **Run with highest privileges**.

Click **OK** to save.

### Start the service

In the Task Scheduler list, right-click **qui** and click **Run**.

:::tip
To restart the service, click **End** and then **Run** in the right sidebar of Task Scheduler.
:::

## Updating

qui has a built-in update command. Stop the scheduled task first. A running task keeps the old version until you restart it.

1. Open **Task Scheduler**, right-click the **qui** task, and click **End**.
2. Run the updater:
   ```powershell
   .\qui.exe update
   ```
3. Right-click the **qui** task again and click **Run** to restart it.

## Reverse proxy (optional)

If you need remote access, run qui behind a reverse proxy like [Caddy](https://caddyserver.com/) or nginx for TLS.

See the [Base URL](../configuration/base-url.md) section for reverse proxy configuration examples.

## Finishing up

When the task runs, access qui at [http://localhost:7476](http://localhost:7476). Add your qBittorrent instances to manage your torrents.
