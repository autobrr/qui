# Architecture Notes

Internal reference for agents and maintainers. Read this before changing cross-module data flow, service boundaries, API routing, or long-lived architecture. For user-facing docs, use `documentation/docs/`.

## Module Map

- `cmd/qui/main.go`: CLI entrypoint for serve, config generation, user creation, and other commands.
- `internal/api/`: HTTP handlers, middleware, and routing.
- `internal/qbittorrent/`: qBittorrent client pool and sync manager. It also owns the folder cleanup worker, which removes, through the `fsops` backends, the leftover folders after qui's deletes and moves, and gets orphan scan's ignore matcher through an interface.
- `internal/services/`: domain services such as cross-seed, Jackett/Torznab, reannounce, and tracker rules.
- `internal/fsops/`: filesystem backend abstraction. Service callsites moved from direct `os.*` calls to an `fsops.Backend` (migration in #1915) resolved per instance via `fsops.Pool`. Some direct `os.*` calls remain under `internal/services`, for example in `crossseed/partial_pool.go` and `trackericons`. The pool returns the local backend for instances with local filesystem access, returns the SFTP-backed remote backend (`internal/fsops/remote`; implements writes over sftp and advertises Write since #2942, reflinks unsupported) for an instance with SSH credentials and a confirmed host-key pin, and returns a noop backend otherwise (every filesystem op fails with `ErrNoFilesystemAccess`, or with the context error). The remote backend slots in at the pool (design: `docs/remote-backend-design.md`).
- `internal/sshpool/`: SSH connections for remote instances. One-shot dials with the stored key, host-key pinning against the confirmed pin and the read-only capability probe serve `ssh-test`; `Pool` keeps one persistent SSH+SFTP connection per instance for the remote backend, with keepalive, reconnect backoff, and an in-memory refusal memo for a mismatched or unreadable pin that a changed pin or a restart clears; after a restart, the next dial refuses an unchanged wrong key again.
- `internal/proxy/`: reverse proxy support for external apps.
- `internal/services/activity/`: in-process publish and subscribe for server activity. A service publishes a kind and IDs, never data. `/api/stream` sends each event to the browser as an `activity` event, and the frontend invalidates the matching react-query key prefix (`web/src/lib/activity-invalidation.ts`). The hub drops events while no stream is open, so after a reconnect the frontend invalidates every activity feed. The files, peers, and pieces of one torrent stay on polling, because qBittorrent does not push them. The file list comes from a database cache that can be up to five minutes old. The files endpoint skips the cache when the request has `refresh=1`.
- `internal/backups/`: scheduled snapshots.
- `internal/database/`: SQLite/Postgres migrations and database setup.
- `internal/models/`: data models and store interfaces.
- `pkg/`: shared utilities.
- `web/src/`: frontend. Its stack and rules are in `web/AGENTS.md`.

## Core Data Flow

1. `SyncManager` fetches maindata from qBittorrent only when something asks for it. While a browser has an SSE stream open for an instance, a loop syncs that instance about every 2 seconds, and backs off to at most 30 seconds after errors. At other times, a read through a getter without `Unchecked` in its name syncs when the cache is more than 2 seconds old. A refresh hint after a write starts a sync 200 ms after the last hint, and at most 1 second after the first. The tracker health loop reads each connected instance every 60 seconds, so with no browser open, the cache of a reachable instance is at most about 60 seconds old.
   - A getter without `Unchecked` in its name syncs on the goroutine of the caller and waits for qBittorrent to answer. The comment in `ensureFreshData` in go-qbittorrent says that the sync is async, but the code calls `Sync` and waits. A path that must not wait uses the `Unchecked` getters (see `mainDataModeForRequest` in `internal/qbittorrent/maindata.go`).
   - go-qbittorrent calls `OnUpdate` and `OnError` inside the singleflight call of the sync. If a callback calls `Sync`, or a getter without `Unchecked` while the data is stale, the callback deadlocks. Run such work on a new goroutine.
2. Torrent state is cached in memory with delta updates.
3. Frontend reads state through REST APIs and receives live updates through SSE.
4. Cross-seed services react to torrent completion and search/match events.

