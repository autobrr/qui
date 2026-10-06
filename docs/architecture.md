# Architecture Notes

Internal reference for agents and maintainers. Read this before changing cross-module data flow, service boundaries, API routing, or long-lived architecture. For user-facing docs, use `documentation/docs/`.

## Module Map

- `cmd/qui/main.go`: CLI entrypoint for serve, config generation, user creation, and other commands.
- `internal/api/`: HTTP handlers, middleware, and routing.
- `internal/qbittorrent/`: qBittorrent client pool and sync manager.
- `internal/services/`: domain services such as cross-seed, Jackett/Torznab, reannounce, and tracker rules.
- `internal/fsops/`: filesystem backend abstraction. Service callsites are migrating from direct `os.*` calls to an `fsops.Backend` (migration in #1915) resolved per instance via `fsops.Pool`. The pool returns the local backend for instances with local filesystem access, returns the SFTP-backed remote backend (`internal/fsops/remote`; implements writes over sftp but advertises only Read until #2942, reflinks unsupported) for an instance with SSH credentials and a confirmed host-key pin, and returns a noop backend otherwise (every filesystem op fails with `ErrNoFilesystemAccess`, or with the context error). The remote backend slots in at the pool (design: `docs/remote-backend-design.md`).
- `internal/sshpool/`: SSH connections for remote instances. One-shot dials with the stored key, host-key pinning against the confirmed pin and the read-only capability probe serve `ssh-test`; `Pool` keeps one persistent SSH+SFTP connection per instance for the remote backend, with keepalive, reconnect backoff, and an in-memory refusal memo for a mismatched or unreadable pin that only a changed pin clears.
- `internal/proxy/`: reverse proxy support for external apps.
- `internal/backups/`: scheduled snapshots.
- `internal/database/`: SQLite/Postgres migrations and database setup.
- `internal/models/`: data models and store interfaces.
- `pkg/`: shared utilities.
- `web/src/`: frontend. Its stack and rules are in `web/AGENTS.md`.

## Core Data Flow

1. `SyncManager` polls qBittorrent instances through `ClientPool`.
2. Torrent state is cached in memory with delta updates.
3. Frontend reads state through REST APIs and receives live updates through SSE.
4. Cross-seed services react to torrent completion and search/match events.

