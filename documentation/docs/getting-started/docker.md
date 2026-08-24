---
sidebar_position: 3
title: Docker
description: Run qui in Docker with compose or standalone.
---

import CodeBlock from '@theme/CodeBlock';
import DockerCompose from '!!raw-loader!@site/../distrib/docker/docker-compose.yml';
import DockerComposePostgres from '!!raw-loader!@site/../distrib/docker/docker-compose.postgres.yml';
import LocalFilesystemDocker from "../_partials/_local-filesystem-docker.mdx";

# Docker

## Docker Compose

<CodeBlock language="yaml" title="docker-compose.yml">{DockerCompose}</CodeBlock>

```bash
docker compose up -d
```

## Docker Compose (Postgres)

<CodeBlock language="yaml" title="docker-compose.postgres.yml">{DockerComposePostgres}</CodeBlock>

```bash
docker compose -f docker-compose.postgres.yml up -d
```

## Standalone

```bash
docker run -d \
  -p 7476:7476 \
  -v $(pwd)/config:/config \
  ghcr.io/autobrr/qui:latest
```

## macOS Container

On macOS, [Apple Container](https://github.com/apple/container/releases) runs the same image. Create the host folders first, then use `container` in place of `docker`:

```bash
container run -d \
  -p 7476:7476 \
  -v $(pwd)/config:/config \
  ghcr.io/autobrr/qui:latest
```

## Permissions (PUID/PGID/UMASK)

By default the container runs as root. To run as a specific user and make sure files in `/config` have correct ownership, set both `PUID` and `PGID` environment variables (required together).

When both are set, the entrypoint will:

1. Create a user and group with the specified IDs
2. Recursively `chown -R` the `/config` directory
3. Run qui as that user

This makes sure the database, logs, and directories created by cross-seed hardlink mode get the expected ownership.

Optional: `UMASK` controls default permissions for files and directories qui creates (database, logs, backups, hardlink-mode directories). Common values:

- `022` - owner read/write, group/others read-only (typical default)
- `002` - owner and group read/write, others read-only (group-writable)
- `077` - owner only, no group/others access (private)

```yaml title="docker-compose.yml"
services:
  qui:
    image: ghcr.io/autobrr/qui:latest
    environment:
      PUID: "1000"
      PGID: "1000"
      UMASK: "002" # optional
    volumes:
      - ./qui:/config
    ports:
      - "7476:7476"
```

```bash
docker run -d \
  -e PUID=1000 \
  -e PGID=1000 \
  -e UMASK=002 \
  -p 7476:7476 \
  -v $(pwd)/config:/config \
  ghcr.io/autobrr/qui:latest
```

:::note
`user:` in compose and `--user` in docker run bypass the entrypoint's chown and privilege-drop behavior. Use `PUID`/`PGID` instead for correct `/config` ownership handling.
:::

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
10. Add environment variables `PUID` = `99` and `PGID` = `100` so the entrypoint chowns `/config` and runs qui as `nobody` (do not use `--user`, it bypasses this)
11. (Optional) add environment variables for advanced settings (e.g., `QUI__BASE_URL`, `QUI__LOG_LEVEL`, `TZ`)
12. Click **Apply** to pull the image and start the container

The `/config` mount stores `config.toml`, logs, tracker icon cache, and other runtime assets. If you use the default SQLite engine, `qui.db` is stored there too. Point it at your preferred appdata share so settings persist across upgrades.

If the app logs to stdout, check logs via Docker → qui → Logs; if it writes to files, they'll be under `/config`.

### Updating

- Use Unraid's **Check for Updates** action to pull a newer `latest` image
- If you pinned a specific version tag, edit the repository field to the new tag when you're ready to upgrade
- Restart the container if needed after the image update so the new binary is loaded

## Updating

```bash
docker compose pull && docker compose up -d
```
