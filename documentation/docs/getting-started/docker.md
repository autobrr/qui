---
sidebar_position: 3
title: Docker
description: Run qui in Docker with compose or standalone.
---

import CodeBlock from '@theme/CodeBlock';
import DockerCompose from '!!raw-loader!@site/../distrib/docker/docker-compose.yml';
import LocalFilesystemDocker from '@site/docs/_partials/_local-filesystem-docker.mdx';

# Docker

## Docker Compose

<CodeBlock language="yaml" title="docker-compose.yml">{DockerCompose}</CodeBlock>

```bash
docker compose up -d
```

## Standalone

```bash
docker run -d \
  -p 7476:7476 \
  -v $(pwd)/config:/config \
  ghcr.io/autobrr/qui:latest
```

## Local Filesystem Access

<LocalFilesystemDocker />

## Unraid

Our release workflow builds multi-architecture images (`linux/amd64`, `linux/arm64`, and friends) and publishes them to `ghcr.io/autobrr/qui`, so the container should work on Unraid out of the box.

### Deploy from the Docker tab

1. Open **Docker → Add Container**
2. Set **Name** to `qui`
3. Set **Repository** to `ghcr.io/autobrr/qui:latest`
4. Keep the default **Network Type** (`bridge` works for most setups)
5. Add a port mapping: **Host port** `7476` → **Container port** `7476`
6. Add a path mapping: **Container Path** `/config` → **Host Path** `/mnt/user/appdata/qui`
7. Enable **Advanced View** (top right)
8. Set **Icon URL** to `https://raw.githubusercontent.com/autobrr/qui/main/web/public/icon.png`
9. Set **WebUI** to `http://[IP]:[PORT:7476]`
10. Set **Extra Parameters** to `--user="99:100"` (if you ran qui without this before you will need to change the ownership for the config and hardlink folders to `nobody`)
11. (Optional) add environment variables for advanced settings (e.g., `QUI__BASE_URL`, `QUI__LOG_LEVEL`, `TZ`)
12. Click **Apply** to pull the image and start the container

The `/config` mount stores `config.toml`, the SQLite database, and logs. Point it at your preferred appdata share so settings persist across upgrades.

If the app logs to stdout, check logs via Docker → qui → Logs; if it writes to files, they'll be under `/config`.

### Updating

- Use Unraid's **Check for Updates** action to pull a newer `latest` image
- If you pinned a specific version tag, edit the repository field to the new tag when you're ready to upgrade
- Restart the container if needed after the image update so the new binary is loaded

## Updating

```bash
docker compose pull && docker compose up -d
```
