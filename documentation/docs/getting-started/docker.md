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

## Permissions

By default the container runs as root. You can run qui as a different user in two ways. Use one or the other, not both.

With either method, qui needs write access to more than `/config`. Cross-seed hardlink and reflink mode create files in their base directory, and orphan scan deletes from your scan paths. Those paths live on the data volumes (see [Local Filesystem Access](#local-filesystem-access)), so run qui as the user that owns that data, or as a member of its group.

### `user:` (standard Docker)

Set `user:` in compose, or `--user` in docker run. Docker starts the container as that user.

```yaml title="docker-compose.yml"
services:
  qui:
    image: ghcr.io/autobrr/qui:latest
    user: "1000:1000"
    volumes:
      - ./qui:/config
    ports:
      - "7476:7476"
```

With this method, make sure that the host folder mounted at `/config` is writable for that user:

```bash
chown -R 1000:1000 ./qui
```
### PUID/PGID (automatic ownership)

Set both `PUID` and `PGID` environment variables (required together). The entrypoint then:

1. Creates a user and group with those IDs
2. Runs `chown -R` on the `/config` directory
3. Runs qui as that user

The result is the same as `user:`, but the entrypoint corrects the ownership of `/config` for you. This helps when `/config` already contains root-owned files from an earlier run, or when the host folder has the wrong owner.

```yaml title="docker-compose.yml"
services:
  qui:
    image: ghcr.io/autobrr/qui:latest
    environment:
      PUID: "1000"
      PGID: "1000"
    volumes:
      - ./qui:/config
    ports:
      - "7476:7476"
```

```bash
docker run -d \
  -e PUID=1000 \
  -e PGID=1000 \
  -p 7476:7476 \
  -v $(pwd)/config:/config \
  ghcr.io/autobrr/qui:latest
```

:::note
Do not combine `user:` with `PUID`/`PGID`. The entrypoint can only create users and change ownership when the container starts as root. If you switch to `PUID`/`PGID`, remove any `user:` or `--user` setting first.
:::

The entrypoint walks `/config` only, never your data volumes, so a wrong `PUID` cannot chown your media library. That also means a switch from root needs one manual step: if qui already created hardlink or reflink trees as root, chown those directories once yourself:

```bash
find /data/cross-seed -type d -exec chown 1000:1000 {} +
```

Directories only: hardlinked files share their inode with the source download, so a recursive `chown -R` here would change the owner of your library files too. qui only needs write access to the directories.

### UMASK

Optional, works with both methods. qui reads `UMASK` at startup and applies it to the files and directories that it creates, for example the cross-seed hardlink and reflink trees. If the value is not valid octal, qui logs a warning and keeps the inherited umask. Common values:

- `022` - owner read/write, group/others read-only (typical default)
- `002` - owner and group read/write, others read-only (group-writable)
- `077` - owner only, no group/others access (private)

Two exceptions:

- qui always creates security-sensitive files (the database, `config.toml`, backup manifests) owner-only (`0600`), regardless of `UMASK`.
- Hardlinked files share the inode with the source file. They keep the owner and permissions of the original download. See [Directory permissions and umask](../features/cross-seed/troubleshooting.md#directory-permissions-and-umask).

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
10. Add environment variables `PUID` = `99` and `PGID` = `100`. The entrypoint then corrects the ownership of `/config` and runs qui as uid 99 (`nobody` on Unraid). If **Extra Parameters** contains `--user`, remove it first, and if qui ran as root before, fix your data directories once (see [Permissions](#permissions))
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
