---
sidebar_position: 1
title: Configuration Reference
---

# Configuration Reference

qui supports configuration via:

- `config.toml` (auto-created on first run, or manually via `qui generate-config`)
- environment variables (`QUI__...`) to override `config.toml`

This page documents both in one place.

## Precedence

Highest wins:

1. `QUI__*_FILE` (for supported secrets)
2. `QUI__*` environment variables
3. `config.toml`
4. built-in defaults

## Config File Location

Default `config.toml` locations:

- Linux/macOS: `~/.config/qui/config.toml`
- Windows: `%APPDATA%\\qui\\config.toml`

Override with `--config-dir`:

- directory path: `--config-dir /path/to/config/` (uses `/path/to/config/config.toml`)
- file path (back-compat): `--config-dir /path/to/custom.toml`

## Notes On Reloading

qui watches `config.toml` for changes. Some settings are applied immediately (for example logging, tracker icon fetching, and auth-disabled settings). For anything else, restart qui after changes to be safe.

## Settings

| TOML key | Environment variable | Type | Default | Notes |
|---|---|---:|---|---|
| `host` | `QUI__HOST` | string | `localhost` (or `0.0.0.0` in containers) | Bind address for the main HTTP server. |
| `port` | `QUI__PORT` | int | `7476` | Port for the main HTTP server. |
| `baseUrl` | `QUI__BASE_URL` | string | `/` | Serve qui from a subdirectory (example: `/qui/`). |
| `sessionSecret` | `QUI__SESSION_SECRET` / `QUI__SESSION_SECRET_FILE` | string | auto-generated | WARNING: changing breaks decryption of stored instance passwords; you must re-enter them in the UI. |
| `logLevel` | `QUI__LOG_LEVEL` | string | `INFO` | `ERROR`, `DEBUG`, `INFO`, `WARN`, `TRACE`. Applied immediately. |
| `logPath` | `QUI__LOG_PATH` | string | empty | If empty: logs to stdout. Relative paths resolve relative to the config directory. Applied immediately. |
| `logMaxSize` | `QUI__LOG_MAX_SIZE` | int | `50` | MiB threshold before rotation. Applied immediately. |
| `logMaxBackups` | `QUI__LOG_MAX_BACKUPS` | int | `3` | Rotated files retained. `0` keeps all. Applied immediately. |
| `dataDir` | `QUI__DATA_DIR` | string | empty | If empty: uses the directory containing `config.toml`. Database `qui.db` lives here. Restart recommended. |
| `checkForUpdates` | `QUI__CHECK_FOR_UPDATES` | bool | `true` | Controls update checks and UI indicators. Restart recommended. |
| `trackerIconsFetchEnabled` | `QUI__TRACKER_ICONS_FETCH_ENABLED` | bool | `true` | Disable to prevent remote tracker favicon fetches. Applied immediately. |
| `crossSeedRecoverErroredTorrents` | `QUI__CROSS_SEED_RECOVER_ERRORED_TORRENTS` | bool | `false` | When enabled, cross-seed automation attempts recovery (pause, recheck, resume) for errored/missingFiles torrents. Can add 25+ minutes per torrent. Restart recommended. |
| `pprofEnabled` | `QUI__PPROF_ENABLED` | bool | `false` | Enables pprof server on `:6060` (`/debug/pprof/`). Restart required. |
| `metricsEnabled` | `QUI__METRICS_ENABLED` | bool | `false` | Enables a Prometheus metrics server (separate port). Restart required. |
| `metricsHost` | `QUI__METRICS_HOST` | string | `127.0.0.1` | Metrics server bind address. Restart required. |
| `metricsPort` | `QUI__METRICS_PORT` | int | `9074` | Metrics server port. Restart required. |
| `metricsBasicAuthUsers` | `QUI__METRICS_BASIC_AUTH_USERS` | string | empty | Optional basic auth: `user:bcrypt_hash` or `user1:hash1,user2:hash2`. Restart required. |
| `externalProgramAllowList` | (none) | string[] | empty list | Restricts which executables can be launched from the UI. Only configurable via `config.toml` (no env override). |
| `authDisabled` | `QUI__AUTH_DISABLED` | bool | `false` | Disable all built-in authentication. **Both** this and `I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA` must be `true` for auth to be disabled. See [Authentication](#authentication) below. Applied on config reload. |
| `I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA` | `QUI__I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA` | bool | `false` | Required confirmation for `authDisabled`. Acknowledges that running without authentication can lead to unauthorized access to your torrent clients and potential bans from private trackers. Applied on config reload. |
| `authDisabledAllowedCIDRs` | `QUI__AUTH_DISABLED_ALLOWED_CIDRS` | string[] | empty list | Required when auth is disabled. Restricts access to specific client IPs/CIDRs. Entries may be canonical CIDRs or single IPs. Applied on config reload. |
| `oidcEnabled` | `QUI__OIDC_ENABLED` | bool | `false` | Enable OpenID Connect authentication. Restart required. |
| `oidcIssuer` | `QUI__OIDC_ISSUER` | string | empty | OIDC issuer URL. Restart required. |
| `oidcClientId` | `QUI__OIDC_CLIENT_ID` | string | empty | OIDC client ID. Restart required. |
| `oidcClientSecret` | `QUI__OIDC_CLIENT_SECRET` / `QUI__OIDC_CLIENT_SECRET_FILE` | string | empty | OIDC client secret. Restart required. |
| `oidcRedirectUrl` | `QUI__OIDC_REDIRECT_URL` | string | empty | Must match the provider redirect URI (include `baseUrl` when reverse proxying). Restart required. |
| `oidcDisableBuiltInLogin` | `QUI__OIDC_DISABLE_BUILT_IN_LOGIN` | bool | `false` | Hide local username/password form when OIDC is enabled. Restart required. |

## Authentication

To disable qui's built-in authentication, all of the following are required:

```bash
QUI__AUTH_DISABLED=true
QUI__I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA=true
QUI__AUTH_DISABLED_ALLOWED_CIDRS=127.0.0.1/32,192.168.1.0/24
```

The second variable exists as an explicit acknowledgement of the risks.

`QUI__AUTH_DISABLED_ALLOWED_CIDRS` is mandatory and acts as a hard IP allowlist. If auth is disabled and the value is missing/invalid, qui will refuse to start and reject invalid live reloads.

Entries can be:

- Canonical CIDR ranges (`192.168.1.0/24`)
- Single IPs (`10.0.0.5`), automatically treated as `/32` (IPv4) or `/128` (IPv6)

Non-canonical CIDRs with host bits set (for example `10.0.0.5/8`) are rejected.

`oidcEnabled` and auth-disabled mode cannot be enabled at the same time.

When authentication is disabled:

- Requests are allowed only if the direct client IP matches `authDisabledAllowedCIDRs`.
- `/api/auth/me` returns a synthetic `admin` user so the frontend works without login.
- `/api/auth/validate` returns a synthetic `admin` user so callback/session checks work without login.
- The setup screen is skipped entirely.

**Only use this if qui is behind a reverse proxy that already handles authentication** (e.g., Authelia, Authentik, Caddy with forward_auth).

:::danger Private tracker risks
If you use private trackers, running qui without authentication is especially dangerous. Anyone with network access can control your torrent clients — adding, removing, or modifying torrents. Actions performed by unauthorized users (hit-and-runs, ratio manipulation, uploading unwanted content) can get your accounts permanently banned from private trackers, with no way to recover.
:::

If `QUI__AUTH_DISABLED` is set without `QUI__I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA`, qui will log a warning and keep authentication enabled.

## Example `config.toml`

```toml
host = "0.0.0.0"
port = 7476
baseUrl = "/qui/"

logLevel = "INFO"
logPath = "log/qui.log"
logMaxSize = 50
logMaxBackups = 3

trackerIconsFetchEnabled = false

externalProgramAllowList = [
  "/usr/local/bin",
  "/home/user/bin/my-script",
]
```
