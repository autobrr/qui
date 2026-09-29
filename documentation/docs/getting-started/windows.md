---
sidebar_position: 4
title: Run qui on Windows with the Tray or as a scheduled task
sidebar_label: Windows
description: Install qui on Windows and run it in the background, with an icon in the notification area or as a scheduled task.
---

# Windows installation

This guide explains how to install qui on Windows and run it in the background. There are two ways to do this. Use one of them, never both, or two copies of qui start and the second one fails because the port is in use.

- **The Tray** runs qui in your logon session, with an icon in the notification area. Use it when you log on to the computer where qui runs. See [Run qui with the Tray](#run-qui-with-the-tray).
- **A scheduled task** runs qui without a logon session and shows no icon. Use it for a headless computer, or when qui must run before anyone logs on. See [Create a Windows task](#create-a-windows-task).

## Download

1. Download the latest Windows release from [GitHub Releases](https://github.com/autobrr/qui/releases/latest).
   - For most systems, download `qui_x.x.x_windows_x86_64.zip`.
2. Extract the archive and place `qui.exe` and `qui-tray.exe` in a directory, for example `C:\qui`. Keep the two files in the same directory: an update replaces both.

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

4. If qui works, stop the process with `Ctrl+C`. Then run qui with the Tray, or configure it as a background task.

### Configuration

qui stores its configuration and runtime data in `%APPDATA%\qui\` by default. With the default SQLite engine, qui stores `qui.db` there too. For more details, see the [Configuration](../configuration/environment.md) section.

## Run qui with the Tray

`qui-tray.exe` runs qui without a console window. While qui runs, the qui icon shows in the notification area of the taskbar.

1. Double-click `qui-tray.exe`. The qui icon shows in the notification area. If Windows hides the icon, click the arrow next to the notification area to see it.
2. Click the icon to open qui in your browser.

`qui-tray.exe` accepts the same flags as `qui.exe serve`, for example `--config-dir` and `--data-dir`. It does not accept other commands. Use `qui.exe` for commands such as `update` or `create-user`.

### The Tray menu

Right-click the qui icon to open the menu:

- **Open qui** opens qui in your default browser. A click on the icon does the same.
- **Restart** restarts qui. The icon goes away for a moment and then comes back. qui refuses a Restart while an update installs.
- **Open config folder** opens the folder that holds the configuration, the database, and the log file.
- **Start with Windows** starts `qui-tray.exe` when you log on, with the flags that it runs with now. A check mark shows that it is on. It needs no administrator rights. It applies only to your Windows account.
- **Quit** stops qui.

Hover over the icon to see the qui version and the address.

### Logs

`qui-tray.exe` has no console. It writes the log to `log\qui.log` in the config folder when you did not set `logPath`. If you set `logPath`, qui uses it. You can also read the live log in the web UI.

When qui cannot start, for example because another program uses the port, an error dialog shows the reason.

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
- Under **Settings**, clear **Stop the task if it runs longer than**. Windows sets this to 3 days by default and stops qui when the limit is reached.

Click **OK** to save.

### Start the service

In the Task Scheduler list, right-click **qui** and click **Run**.

:::tip
To restart the service, click **End** and then **Run** in the right sidebar of Task Scheduler.
:::

## Restart from qui

qui can restart itself through the API (`POST /api/system/restart`). On Windows, `qui serve` starts two `qui.exe` processes. The first process only supervises. The second process serves qui. When qui restarts, only the second process stops and starts again.

- The task shows **Running** during and after the restart.
- **End** stops both processes.
- **Run** does not start a second copy while qui runs. The task setting **If the task is already running, then the following rule applies: Do not start a new instance** controls this. It is the default.

A task that you created with the steps above needs no change.

## Updating

### Update from the web UI

Click **Install update** in the update banner or in **Settings → Application**. You do not have to stop the task or quit the Tray. qui replaces `qui.exe` and `qui-tray.exe` and restarts on the new version, and the task stays **Running**. For the conditions and the rollback, see [Update from the web UI](./installation.md#update-from-the-web-ui).

### Update from the shell

qui has a built-in update command. It replaces `qui.exe` and `qui-tray.exe`. Stop the scheduled task or quit the Tray first. A running qui keeps the old version until you restart it.

1. Open **Task Scheduler**, right-click the **qui** task, and click **End**.
2. Run the updater:
   ```powershell
   .\qui.exe update
   ```
3. Right-click the **qui** task again and click **Run** to restart it.

If you use the Tray, click **Quit** in the Tray menu instead of step 1, and start `qui-tray.exe` again instead of step 3.

### Roll back an update

The update keeps the previous version as `qui-v<old version>.bak.exe` next to `qui.exe`, and as `qui-tray-v<old version>.bak.exe` next to `qui-tray.exe`. Each update deletes older backups. The first `qui.exe` process still runs from the file that it started from, so that backup stays until the task stops.

1. Open **Task Scheduler**, right-click the **qui** task, and click **End**.
2. In **Command Prompt** or **PowerShell**, move the backup back. Replace `1.30.0` with the version in the backup file name:
   ```bat
   cmd /c move /Y "C:\qui\qui-v1.30.0.bak.exe" "C:\qui\qui.exe"
   cmd /c move /Y "C:\qui\qui-tray-v1.30.0.bak.exe" "C:\qui\qui-tray.exe"
   ```
3. Right-click the **qui** task and click **Run**.

If you use the Tray, click **Quit** in the Tray menu before step 2, and start `qui-tray.exe` again after it.

:::warning
If you want a full rollback, back up `%APPDATA%\qui\` before you update. A new version can migrate the database, which changes its structure. The old version cannot always read a migrated database, and a move of the backup binary does not undo a migration.
:::

## Reverse proxy (optional)

If you need remote access, run qui behind a reverse proxy like [Caddy](https://caddyserver.com/) or nginx for TLS.

See the [Base URL](../configuration/base-url.md) section for reverse proxy configuration examples.

## Finishing up

When the task runs, access qui at [http://localhost:7476](http://localhost:7476). Add your qBittorrent instances to manage your torrents.
