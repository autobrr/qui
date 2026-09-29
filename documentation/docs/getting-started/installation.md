---
sidebar_position: 1
title: Install qui, the web UI for qBittorrent
sidebar_label: Installation
description: Install qui on Linux with one command, download the binary for Linux, macOS, or Windows, or run the Docker image.
---

# Installation

## Quick install (Linux x86_64)

```bash
# Download and extract the latest release
wget $(curl -s https://api.github.com/repos/autobrr/qui/releases/latest | grep browser_download_url | grep linux_x86_64 | cut -d\" -f4)
```

### Unpack

Extract the archive to `/usr/local/bin`:

```bash
tar -C /usr/local/bin -xzf qui*.tar.gz qui
```

If the command fails with a permission error, run it again with `sudo`. If you do not have root, or you are on a shared system, extract qui to a directory in your home directory, for example `~/.bin`.

## Manual download

Download the latest release for your platform from the [releases page](https://github.com/autobrr/qui/releases). Windows users should follow the [Windows guide](./windows.md). For containers, see the [Docker guide](./docker.md), and for shared seedboxes, the [seedbox installers](./seedbox-installers.md).

On Linux or macOS, extract the archive:

```bash
tar -xzf qui*.tar.gz qui
```

## Run

If you extracted the archive to `/usr/local/bin`, the binary is on your PATH:

```bash
qui serve
```

If you extracted to `~/.bin` or downloaded the archive by hand, change to that directory first:

```bash
chmod +x qui
./qui serve
```

The web interface is available at http://localhost:7476. To change the port or other settings, see [environment variables](../configuration/environment.md).

## Updating

### Update from the web UI

When a new release is available, click **Install update** in the update banner or in the **Update Status** row of **Settings → Application**. The dialog shows the current version, the new version, and a link to the release notes. After you confirm, qui downloads the release, checks its signature, replaces its binary, and restarts. The page reloads when qui is back.

qui shows the **Install update** button only when it can replace its own binary:

- qui does not run in a container. To update a container, pull a new image.
- The running version is a release build, not a local or develop build.
- qui can write to the directory of its binary. If root owns that directory and qui runs as another user, use `sudo qui update`.
- Update checks are on (`checkForUpdates`), and `disableSelfUpdate` is not `true`. See the [configuration reference](../configuration/reference.md).

If the update fails before qui replaces its binary, the dialog shows the error and qui keeps running on the old version.

### Update from the shell

The `qui update` command downloads and installs the latest release:

```bash
qui update
```

If you installed qui to `/usr/local/bin` with `sudo`, run `sudo qui update`. If the binary is not on your PATH, run `./qui update` from its directory.

### Roll back an update

Both update methods keep the previous binary as `qui-v<old version>.bak` in the directory of the binary. Each update deletes older backups. The web UI shows the exact rollback command when qui does not answer 60 seconds after an update. The shell update prints it.

To roll back, stop qui, move the backup back, and start qui again. Replace `1.30.0` with the version in the backup file name, and `/usr/local/bin` with the directory of your binary:

```bash
mv "/usr/local/bin/qui-v1.30.0.bak" "/usr/local/bin/qui"
```

On Windows, follow [Roll back an update](./windows.md#roll-back-an-update) in the Windows guide.

:::warning
A new version can migrate the database. The old version cannot always read a migrated database, and a binary swap does not undo a migration. Back up the qui config directory before you update if you want a full rollback.
:::

## First setup

1. Open your browser at http://localhost:7476
2. Create your account
3. Add your qBittorrent instances
4. Manage your torrents

Create the account before you expose qui to the Internet, and read [Sessions](../configuration/reference.md#sessions) before you put qui behind HTTPS.
