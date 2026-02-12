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
QUI__DATA_DIR=...        # Optional: custom data directory (default: next to config)
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

## External Programs

Configure the allow list from `config.toml`; there is no environment override to keep it read-only from the UI.

## Default Locations

- **Linux/macOS**: `~/.config/qui/config.toml`
- **Windows**: `%APPDATA%\qui\config.toml`
