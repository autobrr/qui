---
sidebar_position: 3
title: Windows
description: Install and run qui on Windows as a background service.
---

# Windows Installation

In this guide we will download qui, set it up, and create a Windows Task so it runs in the background without needing a command prompt window open 24/7.

## Download

1. Download the latest Windows release from [GitHub Releases](https://github.com/autobrr/qui/releases/latest).
   - For most systems, download `qui_x.x.x_windows_amd64.zip`.
2. Extract the archive and place `qui.exe` in a directory, for example `C:\qui`.

:::tip
Avoid placing qui in `C:\Program Files` — it can cause permission issues with the database and config files.
:::

## Initial Setup

1. Open **Command Prompt** or **PowerShell** and navigate to the directory:
   ```powershell
   cd C:\qui
   ```

2. Start qui for the first time to generate the default config and create your account:
   ```powershell
   .\qui.exe serve
   ```

3. Open your browser to [http://localhost:7476](http://localhost:7476) and create your account.

4. Once you've verified it works, stop qui with `Ctrl+C`. We'll set it up as a background task next.

### Configuration

qui stores its configuration and database in `%APPDATA%\qui\` by default. You can override this with the `--config-dir` flag:

```powershell
.\qui.exe serve --config-dir "C:\qui\config"
```

For more details, see the [Configuration](/docs/configuration/reference) section.

## Create a Windows Task

To run qui in the background, we'll use **Task Scheduler**.

1. Press the **Windows key** and search for **Task Scheduler**.
2. Click **Create Basic Task** in the right sidebar.
3. **Name:** `qui` — optionally add a description like: *qui torrent management service*.
4. **Trigger:** Select **When the computer starts**.
5. **Action:** Select **Start a Program**.
   - **Program/script:** Browse to `C:\qui\qui.exe`
   - **Add arguments:** `serve`
   - **Start in:** `C:\qui`
6. Check **Open the Properties dialog** before finishing, then click **Finish**.

### Configure the task properties

In the Properties dialog:

- Under **General**, select **Run whether user is logged on or not**.
- Enter your Windows password when prompted.
- Optionally check **Run with highest privileges** if you encounter permission issues.

Click **OK** to save.

### Start the service

Right-click on **qui** in the Task Scheduler list and click **Run**.

:::tip
To restart the service, click **End** and then **Run** in the right sidebar of Task Scheduler.
:::

## Updating

qui has a built-in update command. You must stop the Task Scheduler job first, otherwise Windows will lock the executable and the update will fail.

1. Open **Task Scheduler**, right-click the **qui** task and click **End**.
2. Run the updater:
   ```powershell
   .\qui.exe update
   ```
3. Right-click the **qui** task again and click **Run** to restart it.

## Reverse Proxy (optional)

For remote access, it's recommended to run qui behind a reverse proxy like [Caddy](https://caddyserver.com/) or nginx for TLS and additional security.

See the [Base URL](/docs/configuration/base-url) section for reverse proxy configuration examples.

## Finishing Up

Once the task is running, qui will be available at [http://localhost:7476](http://localhost:7476). Add your qBittorrent instance(s) and start managing your torrents.
