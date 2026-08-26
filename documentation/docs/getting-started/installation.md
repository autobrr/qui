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
tar -C /usr/local/bin -xzf qui*.tar.gz
```

If the command fails with a permission error, run it again with `sudo`. If you do not have root, or you are on a shared system, extract qui to a directory in your home directory, for example `~/.bin`.

## Manual download

Download the latest release for your platform from the [releases page](https://github.com/autobrr/qui/releases).

## Run

```bash
# Make it executable (Linux/macOS)
chmod +x qui

# Run
./qui serve
```

The web interface is available at http://localhost:7476.

## Updating

The `qui update` command downloads and installs the latest release:

```bash
./qui update
```

## First setup

1. Open your browser at http://localhost:7476
2. Create your account
3. Add your qBittorrent instances
4. Manage your torrents
