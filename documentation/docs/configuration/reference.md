---
sidebar_position: 1
title: Configuration Reference
description: All config.toml options and their defaults.
---

# Configuration Reference

qui reads configuration from two sources:

- `config.toml`, which qui creates on first run or with `qui generate-config`
- environment variables (`QUI__...`) that override `config.toml`

This page documents both in one place.

## Precedence

Highest wins:

1. `QUI__*_FILE` (for supported secrets)
2. `QUI__*` environment variables
3. `config.toml`
4. built-in defaults

## Config file location

Default `config.toml` locations:

- Linux/macOS: `~/.config/qui/config.toml`
- Windows: `%APPDATA%\qui\config.toml`

Override the location with `--config-dir`:

- directory path: `--config-dir /path/to/config/` (uses `/path/to/config/config.toml`)
- file path (backward compatibility): `--config-dir /path/to/custom.toml`

## Reloading

qui watches `config.toml` for changes. qui applies some settings immediately, for example logging, tracker icon fetching, and the auth-disabled settings. If you change other settings, restart qui.

## Settings

| TOML key | Environment variable | Type | Default | Notes |
|---|---|---:|---|---|
| `host` | `QUI__HOST` | string | `localhost` (or `0.0.0.0` in containers) | Bind address for the main HTTP server. |
| `port` | `QUI__PORT` | int | `7476` | Port for the main HTTP server. |
| `baseUrl` | `QUI__BASE_URL` | string | `/` | Serve qui from a subdirectory (example: `/qui/`). qui normalizes the value at startup and adds missing leading and trailing slashes. |
| `corsAllowedOrigins` | `QUI__CORS_ALLOWED_ORIGINS` | string[] | empty list | Explicit CORS allowlist. An empty list disables CORS. Origins must match `http(s)://host[:port]`. qui rejects wildcards and normalizes default ports. Restart required. |
| `sessionSecret` | `QUI__SESSION_SECRET` / `QUI__SESSION_SECRET_FILE` | string | auto-generated | WARNING: a changed value breaks decryption of stored instance passwords. You must enter them again in the UI. |
| `logLevel` | `QUI__LOG_LEVEL` | string | `DEBUG` | `ERROR`, `DEBUG`, `INFO`, `WARN`, `TRACE`. `DEBUG` records sufficient detail to diagnose most reports. `TRACE` adds per-request and per-sync-tick detail and makes the file grow quickly. qui applies changes immediately. |
| `logPath` | `QUI__LOG_PATH` | string | empty | If empty, qui logs to stdout. qui resolves relative paths against the config directory. qui applies changes immediately. |
| `logMaxSize` | `QUI__LOG_MAX_SIZE` | int | `50` | Size in MB that starts log rotation. qui applies changes immediately. |
| `logMaxBackups` | `QUI__LOG_MAX_BACKUPS` | int | `10` | Number of rotated files that qui keeps. `0` keeps all. qui compresses rotated files with gzip. A 50 MB qui log file compresses to 2 to 3 MB. qui applies changes immediately. |
| `dataDir` | `QUI__DATA_DIR` | string | empty | If empty, qui uses the directory that contains `config.toml`. qui stores the tracker icon cache here, and backups by default (`<dataDir>/backups`). If `databaseEngine=sqlite`, `qui.db` also lives here. Log files follow `logPath`, which qui resolves against the config directory. Restart qui after a change. |
| `backupDir` | `QUI__BACKUP_DIR` | string | empty | If empty, qui uses `<dataDir>/backups`. Directory for [backup](../features/backups.md) manifests, archives, and cached `.torrent` files. qui resolves relative paths against the config directory. If you change this on an existing install, move the contents of `<dataDir>/backups` to the new directory. Restart required. |
| `customThemesDir` | `QUI__CUSTOM_THEMES_DIR` | string | empty | Directory for sideloaded [custom theme](../features/custom-themes.md) `.css` files. If empty, qui uses `<config-dir>/themes` (auto-created). qui resolves relative paths against the config directory. Listing themes requires premium access. qui applies config changes on the next request. |
| `databaseEngine` | `QUI__DATABASE_ENGINE` | string | `sqlite` | `sqlite` or `postgres`. If you do not migrate, keep `sqlite` on an existing install. Restart required. |
| `databaseDsn` | `QUI__DATABASE_DSN` / `QUI__DATABASE_DSN_FILE` | string | empty | Full Postgres DSN. If `databaseEngine=postgres`, prefer this setting. |
| `databaseHost` | `QUI__DATABASE_HOST` | string | `localhost` | If you do not use `databaseDsn`, this sets the Postgres host. |
| `databasePort` | `QUI__DATABASE_PORT` | int | `5432` | If you do not use `databaseDsn`, this sets the Postgres port. |
| `databaseUser` | `QUI__DATABASE_USER` | string | empty | If you do not use `databaseDsn`, this sets the Postgres user. |
| `databasePassword` | `QUI__DATABASE_PASSWORD` / `QUI__DATABASE_PASSWORD_FILE` | string | empty | If you do not use `databaseDsn`, this sets the Postgres password. |
| `databaseName` | `QUI__DATABASE_NAME` | string | `qui` | If you do not use `databaseDsn`, this sets the Postgres database name. |
| `databaseSSLMode` | `QUI__DATABASE_SSL_MODE` | string | `disable` | Common values: `disable`, `require`, `verify-ca`, `verify-full`. |
| `databaseConnectTimeout` | `QUI__DATABASE_CONNECT_TIMEOUT` | int | `10` | Postgres connect timeout in seconds. |
| `databaseMaxOpenConns` | `QUI__DATABASE_MAX_OPEN_CONNS` | int | `25` | Postgres pool max open connections. |
| `databaseMaxIdleConns` | `QUI__DATABASE_MAX_IDLE_CONNS` | int | `5` | Postgres pool max idle connections. |
| `databaseConnMaxLifetime` | `QUI__DATABASE_CONN_MAX_LIFETIME` | int | `300` | Postgres connection max lifetime in seconds. |
| `qbittorrentTimeout` | `QUI__QBITTORRENT_TIMEOUT` | int | `60` | HTTP timeout in seconds for requests qui makes to qBittorrent instances (sync, health checks). If you run large or slow instances, raise this value. Restart required. |
| `checkForUpdates` | `QUI__CHECK_FOR_UPDATES` | bool | `true` | Controls update checks and UI indicators. qui applies changes on config reload. If you set it with the environment variable, restart qui. |
| `trackerIconsFetchEnabled` | `QUI__TRACKER_ICONS_FETCH_ENABLED` | bool | `true` | Disable this setting to prevent remote tracker favicon fetches. qui applies changes immediately. |
| `crossSeedRecoverErroredTorrents` | `QUI__CROSS_SEED_RECOVER_ERRORED_TORRENTS` | bool | `false` | If enabled, cross-seed automation attempts recovery (pause, recheck, resume) for errored/missingFiles torrents. This process can add 25+ minutes per torrent. Restart qui after a change. |
| `pprofEnabled` | `QUI__PPROF_ENABLED` | bool | `false` | Enables the pprof server (`/debug/pprof/`). Restart required. |
| `pprofAddr` | `QUI__PPROF_ADDR` | string | `127.0.0.1:6060` | pprof server bind address. The default loopback address prevents collisions with other listeners in a shared network namespace (for example a Tailscale sidecar). Restart required. |
| `metricsEnabled` | `QUI__METRICS_ENABLED` | bool | `false` | Enables a Prometheus metrics server on a separate port. Restart required. |
| `metricsHost` | `QUI__METRICS_HOST` | string | `127.0.0.1` | Metrics server bind address. Restart required. |
| `metricsPort` | `QUI__METRICS_PORT` | int | `9074` | Metrics server port. Restart required. |
| `metricsBasicAuthUsers` | `QUI__METRICS_BASIC_AUTH_USERS` | string | empty | Optional basic auth: `user:password` or `user1:password1,user2:password2`. Passwords are plaintext and can contain colons. Usernames cannot contain colons. Commas cannot appear in credentials. Restart required. |
| `externalProgramAllowList` | (none) | string[] | empty list | Restricts which executables qui can launch from the UI. Configure this only in `config.toml` (no env override). |
| `authDisabled` | `QUI__AUTH_DISABLED` | bool | `false` | Disables all built-in authentication. You must set **both** this and `I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA` to `true` to disable auth. See [Authentication](#authentication) below. qui applies changes on config reload. |
| `I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA` | `QUI__I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA` | bool | `false` | Required confirmation for `authDisabled`. Acknowledges that qui without authentication can permit unauthorized access to your torrent clients and can cause private tracker bans. qui applies changes on config reload. |
| `authDisabledAllowedCIDRs` | `QUI__AUTH_DISABLED_ALLOWED_CIDRS` | string[] | empty list | If auth is disabled, this setting is required. Restricts access to specific client IPs and CIDRs. Entries can be canonical CIDRs or single IPs. qui applies changes on config reload. |
| `oidcEnabled` | `QUI__OIDC_ENABLED` | bool | `false` | Enables OpenID Connect authentication. Restart required. |
| `oidcIssuer` | `QUI__OIDC_ISSUER` | string | empty | OIDC issuer URL. Restart required. |
| `oidcClientId` | `QUI__OIDC_CLIENT_ID` | string | empty | OIDC client ID. Restart required. |
| `oidcClientSecret` | `QUI__OIDC_CLIENT_SECRET` / `QUI__OIDC_CLIENT_SECRET_FILE` | string | empty | OIDC client secret. Restart required. |
| `oidcRedirectUrl` | `QUI__OIDC_REDIRECT_URL` | string | empty | Must match the provider redirect URI. If you use a reverse proxy, include `baseUrl`. Restart required. |
| `oidcDisableBuiltInLogin` | `QUI__OIDC_DISABLE_BUILT_IN_LOGIN` | bool | `false` | If OIDC is enabled, this hides the local username and password form. Restart required. |

## Authentication

To disable the built-in authentication of qui, set all of the following:

```bash
QUI__AUTH_DISABLED=true
QUI__I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA=true
QUI__AUTH_DISABLED_ALLOWED_CIDRS=127.0.0.1/32,192.168.1.0/24
```

The second variable is an explicit acknowledgement of the risks.

`QUI__AUTH_DISABLED_ALLOWED_CIDRS` is mandatory and is a hard IP allowlist. If auth is disabled and the value is missing or invalid, qui refuses to start and rejects invalid live reloads.

Entries can be:

- canonical CIDR ranges (`192.168.1.0/24`)
- single IPs (`10.0.0.5`), which qui treats as `/32` (IPv4) or `/128` (IPv6)

qui rejects non-canonical CIDRs with host bits set (for example `10.0.0.5/8`).

You cannot enable `oidcEnabled` and auth-disabled mode at the same time.

When authentication is disabled:

- qui permits requests only from client IPs that match `authDisabledAllowedCIDRs`.
- The built-in health endpoints (`/health`, `/healthz/readiness`, `/healthz/liveness`) still allow loopback probes. The official Docker image healthcheck works without `127.0.0.1/32` or `::1/128` in your reverse proxy allowlist.
- `/api/auth/me` returns a synthetic `admin` user, so the frontend works without login.
- `/api/auth/validate` returns a synthetic `admin` user, so callback and session checks work without login.
- qui skips the setup screen.

**Only use this mode when qui is behind a reverse proxy that already handles authentication** (for example Authelia, Authentik, or Caddy with forward_auth).

:::danger Private tracker risks
If you use private trackers, running qui without authentication creates severe risk. Anyone with network access can add, remove, or modify torrents in your clients. Unwanted uploads, ratio manipulation, or hit-and-runs can cause permanent account bans on private trackers.
:::

If you set `QUI__AUTH_DISABLED` without `QUI__I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA`, qui logs a warning and keeps authentication enabled.

## CORS

By default, qui does not send CORS allow headers. To allow browser requests from another trusted origin, set `corsAllowedOrigins` (or `QUI__CORS_ALLOWED_ORIGINS`) to an explicit allowlist:

```bash
QUI__CORS_ALLOWED_ORIGINS=https://sso.example.com,https://panel.example.com
```

Rules:

- qui allows only explicit origins (`http://` or `https://` + host + optional non-default port)
- qui rejects wildcards (`*`, `https://*.example.com`, and similar patterns)
- qui rejects values with a path, query, fragment, or userinfo
- If a startup value is invalid, qui stops. On live reload, qui logs an invalid value and keeps the running allowlist until a restart.

For SSO proxy setups, configure CORS on the proxy auth endpoints first. See [SSO Proxies and CORS](../advanced/sso-proxy-cors.md).

## Example `config.toml`

```toml
host = "0.0.0.0"
port = 7476
baseUrl = "/qui/"

logLevel = "DEBUG"
logPath = "log/qui.log"
logMaxSize = 50
logMaxBackups = 10

trackerIconsFetchEnabled = false

externalProgramAllowList = [
  "/usr/local/bin",
  "/home/user/bin/my-script",
]
```
