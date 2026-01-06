---
sidebar_position: 1
title: Installation
description: Install qui on Linux with a single command.
---

# Installation

## Quick Install (Linux x86_64)

```bash
# Download and extract the latest release
wget $(curl -s https://api.github.com/repos/autobrr/qui/releases/latest | grep browser_download_url | grep linux_x86_64 | cut -d\" -f4)
```

### Unpack

Run with root or sudo. If you do not have root, or are on a shared system, place the binaries somewhere in your home directory like `~/.bin`.

```bash
tar -C /usr/local/bin -xzf qui*.tar.gz
```

This will extract qui to `/usr/local/bin`. Note: If the command fails, prefix it with `sudo` and re-run again.

## Manual Download

Download the latest release for your platform from the [releases page](https://github.com/autobrr/qui/releases).

## Run

```bash
# Make it executable (Linux/macOS)
chmod +x qui

# Run
./qui serve
```

The web interface will be available at http://localhost:7476

## Updating

qui includes a built-in update command that automatically downloads and installs the latest release:

```bash
./qui update
```

## First Setup

1. Open your browser to http://localhost:7476
2. Create your account
3. Add your qBittorrent instance(s)
4. Start managing your torrents
