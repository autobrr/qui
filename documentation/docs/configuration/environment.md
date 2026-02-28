---
sidebar_position: 2
title: Environment Variables
---

# Environment Variables

Configuration is stored in `config.toml` (created automatically on first run, or manually with `qui generate-config`). You can also use environment variables:

For the complete list (including `config.toml` keys, defaults, and notes), see [Configuration Reference](./reference).

## Server

```bash
QUI__HOST=0.0.0.0        # Listen address
QUI__PORT=7476           # Port number
QUI__BASE_URL=/qui/      # Optional: serve from subdirectory
```

## Security

```bash
QUI__SESSION_SECRET_FILE=...  # Path to file containing secret. Takes precedence over QUI__SESSION_SECRET
QUI__SESSION_SECRET=...       # Auto-generated if not set
```

## Logging

```bash
QUI__LOG_LEVEL=INFO      # Options: ERROR, DEBUG, INFO, WARN, TRACE
QUI__LOG_PATH=...        # Optional: log file path
QUI__LOG_MAX_SIZE=50     # Optional: rotate when log file exceeds N megabytes (default: 50)
QUI__LOG_MAX_BACKUPS=3   # Optional: retain N rotated files (default: 3, 0 keeps all)
```

When `logPath` is set the server writes to disk using size-based rotation. Adjust `logMaxSize` and `logMaxBackups` in `config.toml` or the corresponding environment variables to control the rotation thresholds and retention.

## Storage

```bash
QUI__DATA_DIR=...        # Optional: custom runtime data directory (default: next to config)
```

`QUI__DATA_DIR` is always used for runtime assets (logs, tracker icon cache, etc.). With `QUI__DATABASE_ENGINE=sqlite`, `qui.db` is also stored there.

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

## Cross-Seed

```bash
QUI__CROSS_SEED_RECOVER_ERRORED_TORRENTS=false  # Optional: recover errored/missingFiles torrents; can add ~25+ minutes per torrent (default: false)
```

## Tracker Icons

```bash
QUI__TRACKER_ICONS_FETCH_ENABLED=false  # Optional: set to false to disable remote tracker icon fetching (default: true)
```

## Updates

```bash
QUI__CHECK_FOR_UPDATES=false  # Optional: disable update checks and UI indicators (default: true)
```

## Profiling (pprof)

```bash
QUI__PPROF_ENABLED=true  # Optional: enable pprof server on :6060 (default: false)
```

## Metrics

```bash
QUI__METRICS_ENABLED=true      # Optional: enable Prometheus metrics (default: false)
QUI__METRICS_HOST=127.0.0.1    # Optional: metrics server bind address (default: 127.0.0.1)
QUI__METRICS_PORT=9074         # Optional: metrics server port (default: 9074)
QUI__METRICS_BASIC_AUTH_USERS=user:hash  # Optional: basic auth for metrics (bcrypt hashed)
```

## Authentication

```bash
QUI__AUTH_DISABLED=true                 # Optional: disable built-in auth (default: false)
QUI__I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA=true  # Required confirmation to actually disable auth
QUI__AUTH_DISABLED_ALLOWED_CIDRS=127.0.0.1/32,192.168.1.0/24  # Required when auth is disabled (IPs or CIDRs)
```

Built-in authentication is disabled only when:

- `QUI__AUTH_DISABLED=true`
- `QUI__I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA=true`
- `QUI__AUTH_DISABLED_ALLOWED_CIDRS` is set to one or more allowed IPs/CIDR ranges

If auth is disabled and `QUI__AUTH_DISABLED_ALLOWED_CIDRS` is missing or invalid, qui refuses to start and rejects invalid live reloads.

`QUI__AUTH_DISABLED_ALLOWED_CIDRS` accepts comma-separated entries. Each entry may be a canonical CIDR (`192.168.1.0/24`) or a single IP (`10.0.0.5`, treated as `/32` or `/128`).

Non-canonical CIDRs with host bits set (for example `10.0.0.5/8`) are rejected.

`QUI__OIDC_ENABLED=true` cannot be combined with auth-disabled mode.

Only use this when qui runs behind a reverse proxy that already handles authentication (e.g., Authelia, Authentik, Caddy with forward_auth). See the [Configuration Reference](./reference#authentication) for a full explanation of the risks.

## External Programs

Configure the allow list from `config.toml`; there is no environment override to keep it read-only from the UI.

## Default Locations

- **Linux/macOS**: `~/.config/qui/config.toml`
- **Windows**: `%APPDATA%\qui\config.toml`
