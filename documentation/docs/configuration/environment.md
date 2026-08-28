---
sidebar_position: 2
title: Environment Variables
description: Configure qui with environment variables.
---

# Environment Variables

qui stores configuration in `config.toml`. If you run qui for the first time or run `qui generate-config`, qui creates this file. Environment variables override values in `config.toml`.

For the complete list (including `config.toml` keys, defaults, and notes), see [Configuration Reference](./reference.md).

## Server

```bash
QUI__HOST=0.0.0.0        # Listen address
QUI__PORT=7476           # Port number
QUI__BASE_URL=/qui/      # Optional: serve from subdirectory
```

## CORS

```bash
QUI__CORS_ALLOWED_ORIGINS=https://sso.example.com,https://panel.example.com  # Optional: explicit CORS allowlist (empty disables CORS)
```

`QUI__CORS_ALLOWED_ORIGINS` accepts comma- or space-separated origins. Entries must be explicit `http(s)://host[:port]` values, without wildcards, paths, query strings, fragments, or userinfo.

## Security

```bash
QUI__SESSION_SECRET_FILE=...  # Path to file containing secret. Takes precedence over QUI__SESSION_SECRET
QUI__SESSION_SECRET=...       # Auto-generated if not set
```

## Logging

```bash
QUI__LOG_LEVEL=DEBUG     # Options: ERROR, DEBUG, INFO, WARN, TRACE (default: DEBUG)
QUI__LOG_PATH=...        # Optional: log file path
QUI__LOG_MAX_SIZE=50     # Optional: rotate when the log file is larger than N MB (default: 50)
QUI__LOG_MAX_BACKUPS=10  # Optional: retain N rotated files (default: 10, 0 keeps all)
```

If you set `logPath`, qui writes the log to disk with size-based rotation. Adjust `logMaxSize` and `logMaxBackups` in `config.toml`, or set the equivalent environment variables. qui compresses rotated files with gzip. Measured on real qui logs, a 50 MB file compresses to 2 to 3 MB, so ten backups use about 25 MB. The ratio depends on the content of your log.

Log rotation and retention apply only when you set `logPath`. If you run qui in Docker, it writes logs to stdout by default. Rotation does not apply to these logs. To limit the size of the container log, set `QUI__LOG_PATH`. As an alternative, set the `max-size` option on the container log driver.

## Storage

```bash
QUI__DATA_DIR=...        # Optional: custom runtime data directory (default: next to config)
```

qui uses `QUI__DATA_DIR` for the tracker icon cache and, by default, for backups. With the default `sqlite` engine, qui also stores `qui.db` there. Log files follow `QUI__LOG_PATH`. qui resolves a relative log path against the config directory, not the data directory.

```bash
QUI__BACKUP_DIR=...      # Optional: custom backup directory (default: <dataDir>/backups)
```

`QUI__BACKUP_DIR` sets where qui writes [backup](../features/backups.md) manifests, archives, and cached `.torrent` files. Point it at separate storage, for example a redundant array or a network share. If the data drive fails, your backups remain safe. If you change this on an existing install, move the contents of `<dataDir>/backups` to the new directory.

```bash
QUI__CUSTOM_THEMES_DIR=...  # Optional: directory for sideloaded custom theme .css files (default: <config-dir>/themes)
```

`QUI__CUSTOM_THEMES_DIR` sets where qui reads [custom themes](../features/custom-themes.md) from. It defaults to a `themes` folder next to the config file (`/config/themes` in Docker), and qui creates it automatically. Loading custom themes requires premium access.

### UI preferences

qui stores UI preferences, such as table columns, column sizes, density, filters, and theme, in the database. The browser maintains a local copy to apply these preferences before loading database values.

If these preferences reset after a restart, make sure that qui uses a persistent database. If you use SQLite, keep `qui.db` in persistent storage. If you run Docker, persist `/config` or the directory set by `QUI__DATA_DIR`.

## Database

```bash
QUI__DATABASE_ENGINE=sqlite            # sqlite or postgres (default: sqlite)
QUI__DATABASE_DSN=...                  # Full Postgres DSN (preferred for Postgres)
QUI__DATABASE_HOST=localhost           # Postgres host when not using DATABASE_DSN
QUI__DATABASE_PORT=5432                # Postgres port when not using DATABASE_DSN
QUI__DATABASE_USER=...                 # Postgres user when not using DATABASE_DSN
QUI__DATABASE_PASSWORD=...             # Postgres password when not using DATABASE_DSN
QUI__DATABASE_NAME=qui                 # Postgres database name when not using DATABASE_DSN
QUI__DATABASE_SSL_MODE=disable         # disable, require, verify-ca, verify-full
QUI__DATABASE_CONNECT_TIMEOUT=10       # Connect timeout in seconds
QUI__DATABASE_MAX_OPEN_CONNS=25        # Postgres pool max open connections
QUI__DATABASE_MAX_IDLE_CONNS=5         # Postgres pool max idle connections
QUI__DATABASE_CONN_MAX_LIFETIME=300    # Max connection lifetime in seconds
```

### SQLite or Postgres

Both engines run the same features. For most installs, switching engines provides no performance benefit. qui runs SQLite in WAL mode with a separate read pool, so reads do not block writes. If you need to host the database on a separate machine, or if heavy cross-seed activity and multiple automated instances hit SQLite write limits, select Postgres. You cannot reverse the migration. See [`qui db migrate`](./cli-commands.md).

## qBittorrent connection

```bash
QUI__QBITTORRENT_TIMEOUT=60  # Optional: HTTP timeout in seconds for requests qui makes to qBittorrent instances (default: 60). Raise it for very large or slow instances.
```

## Cross-seed

```bash
QUI__CROSS_SEED_RECOVER_ERRORED_TORRENTS=false  # Optional: recover errored/missingFiles torrents; can add ~25+ minutes per torrent (default: false)
```

## Tracker icons

```bash
QUI__TRACKER_ICONS_FETCH_ENABLED=false  # Optional: set to false to disable remote tracker icon fetching (default: true)
```

## Updates

```bash
QUI__CHECK_FOR_UPDATES=false  # Optional: disable update checks and UI indicators (default: true)
```

## Profiling (pprof)

```bash
QUI__PPROF_ENABLED=true        # Optional: enable pprof server (default: false)
QUI__PPROF_ADDR=127.0.0.1:6060 # Optional: pprof bind address (default: 127.0.0.1:6060)
```

## Metrics

```bash
QUI__METRICS_ENABLED=true      # Optional: enable Prometheus metrics (default: false)
QUI__METRICS_HOST=127.0.0.1    # Optional: metrics server bind address (default: 127.0.0.1)
QUI__METRICS_PORT=9074         # Optional: metrics server port (default: 9074)
QUI__METRICS_BASIC_AUTH_USERS=user:password  # Optional: basic auth for metrics (plaintext password)
```

## Authentication

```bash
QUI__AUTH_DISABLED=true                 # Optional: disable built-in auth (default: false)
QUI__I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA=true  # Required confirmation to actually disable auth
QUI__AUTH_DISABLED_ALLOWED_CIDRS=127.0.0.1/32,192.168.1.0/24  # Required when auth is disabled (IPs or CIDRs)
```

qui disables built-in authentication only when:

- `QUI__AUTH_DISABLED=true`
- `QUI__I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA=true`
- `QUI__AUTH_DISABLED_ALLOWED_CIDRS` contains one or more allowed IPs or CIDR ranges

If you disable authentication and `QUI__AUTH_DISABLED_ALLOWED_CIDRS` is missing or invalid, qui refuses to start and rejects invalid live reloads.

`QUI__AUTH_DISABLED_ALLOWED_CIDRS` accepts comma-separated entries. Each entry can be a canonical CIDR (`192.168.1.0/24`) or a single IP (`10.0.0.5`). qui treats a single IP as `/32` or `/128`.

qui rejects non-canonical CIDRs with host bits set (for example `10.0.0.5/8`).

You cannot combine `QUI__OIDC_ENABLED=true` with auth-disabled mode.

If qui runs behind a reverse proxy that already handles authentication (for example Authelia, Authentik, or Caddy with forward_auth), you can use this mode. See the [Configuration Reference](./reference.md#authentication) for a full explanation of the risks.

The built-in health endpoints (`/health`, `/healthz/readiness`, `/healthz/liveness`) always allow loopback probes. If your allowlist only includes reverse proxy subnets, the official Docker image healthcheck still works.

## External programs

Configure the allow list in `config.toml`. It has no environment override.

## Default locations

- **Linux/macOS**: `~/.config/qui/config.toml`
- **Windows**: `%APPDATA%\qui\config.toml`
