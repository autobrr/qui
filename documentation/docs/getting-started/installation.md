---
sidebar_position: 1
title: Installation
description: Install qui on Linux with a single command.
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

Download the latest release for your platform from the [releases page](https://github.com/autobrr/qui/releases). Windows users should follow the [Windows guide](./windows.md).

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

The web interface is available at http://localhost:7476.

## Updating

The `qui update` command downloads and installs the latest release:

```bash
qui update
```

If you installed qui to `/usr/local/bin` with `sudo`, run `sudo qui update`. If the binary is not on your PATH, run `./qui update` from its directory.

## First setup

1. Open your browser at http://localhost:7476
2. Create your account
3. Add your qBittorrent instances
4. Manage your torrents
