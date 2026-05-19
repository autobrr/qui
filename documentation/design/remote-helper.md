# Remote Helper Design

qui-helper is a small static Go binary that qui pushes to a remote seedbox via SCP and drives over SSH. It enables all filesystem-dependent features (cross-seed inject, dirscan, orphanscan, automations, managed delete cleanup) to work against remote qBittorrent instances.

No inbound port on the seedbox. No pairing tokens. SSH credentials are the only secret, encrypted at rest the same way qBittorrent passwords already are.

## Architecture

```
qui host                                seedbox
+--------------------------+            +---------------------------+
| services/dirscan         |            | qBittorrent               |
| services/orphanscan      |            +---------------------------+
| services/automations     |                    | local FS
| services/crossseed       |                    v
+-----------+--------------+            +---------------------------+
            |                           | ~/data, ~/seed, ...       |
            v                           +---------------------------+
+--------------------------+                    ^
| internal/fsops           |                    | os.* via
| +---------+  +---------+ |                    | os.Root (BENEATH)
| | Local   |  | Remote  | |                    |
| +---------+  +----+----+ |            +---------------------------+
+-------------------+------+            | qui-helper serve --stdio  |
            |                           | (one persistent process)  |
            v                           | pkg/fsexec primitives     |
+--------------------------+            +---------------------------+
| internal/sshpool         |                    ^
| *ssh.Client per instance +--- SSH stdio ------+ NDJSON stdin/stdout
+--------------------------+
```

**Lifecycle:** Lazy connect on first FS op per instance. One persistent `*ssh.Client` + `ssh.Session` runs `qui-helper serve --stdio --root <root1> --root <root2>`. Helper emits a `HelloBanner` (version, capabilities, reflink-supported roots). Commands/results flow as NDJSON on stdin/stdout. SSH keepalive (`ServerAliveInterval=15s`) prevents idle drops. Shutdown: close stdin, helper drains in-flight ops (30s grace), exits.

## Filesystem Operations

This is the complete RPC surface. Anything outside this table stays local to the qui host.

| Feature | Ops used | Volume | Code locations |
|---|---|---|---|
| Cross-seed hardlink inject | MkdirAll, Lstat, Link, Remove, SameFilesystem | 1-100/match | dirscan/inject.go, pkg/hardlinktree |
| Cross-seed reflink inject | MkdirAll, Lstat, CoW clone, Remove | 1-100/match | dirscan/inject.go, pkg/reflinktree |
| Link-tree teardown | Remove (per file) | tens-100 | hardlinktree/reflinktree Rollback |
| Dirscan walk + FileID | WalkDir, Lstat, FileID | 10k+ files | dirscan/scanner.go, dirscan/fileid_index.go |
| Orphanscan walk | WalkDir, Lstat, FileID | 10k+ files | orphanscan/walker.go |
| Orphanscan delete | Lstat, Remove, RemoveAll | tens/run | orphanscan/delete.go |
| Missing-files condition | Stat (batch) | hundreds/cycle | automations/missing_files.go |
| Free-space condition | Statfs | 1/eval | automations/free_space.go |
| Hardlink-scope condition | Lstat + FileID (batch) | 10k+ files | automations/hardlink_index.go |
| Managed delete cleanup | Stat, Remove | tens/delete | qbittorrent/delete_cleanup.go |
| Same-filesystem check | Stat x2 + dev-id compare | 1/inject | pkg/fsutil/samefs.go |

## Backend Interface

`internal/fsops.Backend` abstracts all filesystem operations. Services depend on this interface, not `os.*` directly.

```go
type Backend interface {
    // Read
    Stat(ctx context.Context, path string) (*FileInfo, error)
    StatBatch(ctx context.Context, paths []string) ([]*FileInfo, []error, error)
    Lstat(ctx context.Context, path string) (*LstatInfo, error)
    LstatBatch(ctx context.Context, paths []string) ([]*LstatInfo, []error, error)
    ReadDir(ctx context.Context, path string, maxEntries int) ([]DirEntry, bool, error)
    WalkDir(ctx context.Context, root string, opts WalkOptions) (<-chan WalkEntry, error)
    Statfs(ctx context.Context, path string) (*StatfsResult, error)
    SameFilesystem(ctx context.Context, p1, p2 string) (bool, error)
    FileID(ctx context.Context, path string) (hardlink.FileID, uint64, error)

    // Write
    MkdirAll(ctx context.Context, path string, perm fs.FileMode) error
    Remove(ctx context.Context, path string, opts RemoveOptions) error

    // Atomic tree ops
    HardlinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*TreeCreateResult, error)
    ReflinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*TreeCreateResult, error)
    RemoveTree(ctx context.Context, plan *hardlinktree.TreePlan) error

    // Capabilities
    SupportsReflink(ctx context.Context, path string) (bool, string, error)

    // Diagnostic
    Info(ctx context.Context) (*BackendInfo, error)
    HealthCheck(ctx context.Context) error
}
```

**Implementations:**
- `internal/fsops/local` -- thin adapter over `os.*`, pkg/hardlinktree, pkg/reflinktree, pkg/hardlink, pkg/fsutil
- `internal/fsops/remote` -- translates Backend calls into NDJSON commands dispatched via sshpool
- `internal/fsops.Pool` -- resolves instance ID to the correct Backend (local, remote, or noop)

## Wire Protocol

NDJSON over SSH stdio. Three streams on one `ssh.Session`:

| Stream | Direction | Format | Purpose |
|---|---|---|---|
| stdin | qui -> helper | NDJSON Commands | Op dispatches, cancellations |
| stdout | helper -> qui | NDJSON Results | Op results, walk-stream frames |
| stderr | helper -> qui | structured JSON | Helper logs forwarded to qui |

### Envelopes

```go
type Command struct {
    RequestID string          `json:"requestID"`
    Op        string          `json:"op"`
    Args      json.RawMessage `json:"args"`
    Deadline  string          `json:"deadline,omitempty"` // RFC3339
}

type Result struct {
    RequestID string          `json:"requestID"`
    OK        bool            `json:"ok"`
    Code      string          `json:"code,omitempty"`
    Error     string          `json:"error,omitempty"`
    Payload   json.RawMessage `json:"payload,omitempty"`
    Done      bool            `json:"done,omitempty"`  // last frame for streamed ops
    Frame     int             `json:"frame,omitempty"` // monotonic counter (streamed only)
}

type HelloBanner struct {
    HelperVersion string   `json:"helperVersion"`
    ProtoVersion  string   `json:"protoVersion"` // "1"
    Capabilities  []string `json:"capabilities"`
    AllowedRoots  []string `json:"allowedRoots"`
    ReflinkRoots  []string `json:"reflinkRoots"`
    Platform      string   `json:"platform"`
    Hostname      string   `json:"hostname"`
    PID           int      `json:"pid"`
    StartedAt     string   `json:"startedAt"` // RFC3339
}
```

Non-streaming ops: one Result with `Done: true`. Streaming ops (`fs.walk`): multiple Results with same requestID, final frame has `Done: true`.

### Op Payloads

All types live in `pkg/agent/proto`. Paths are absolute; helper rejects any not under an allowed root.

```go
// fs.stat / fs.lstat
type StatRequest  struct { Paths []string `json:"paths"` }
type StatEntry    struct { Path string; Exists bool; Size int64; ModTime string; IsDir bool; Mode uint32; Err string }
type LstatRequest struct { Paths []string; WantFileID bool; WantNlinks bool }
type LstatEntry   struct { StatEntry; IsSymlink bool; FileID []byte; Nlinks uint64 }

// fs.walk (streaming)
type WalkRequest struct { Root string; SkipHidden bool; IgnoreDirNames []string; IgnorePaths []string; WantFileID bool; WantNlinks bool; MaxEntries int }
type WalkEntry   struct { Path string; RelPath string; IsDir bool; IsSymlink bool; Size int64; ModTime string; Mode uint32; FileID []byte; Nlinks uint64; Err string; Truncated bool }

// fs.statfs
type StatfsRequest  struct { Path string }
type StatfsResponse struct { BytesAvailable int64; BytesTotal int64; Filesystem string }

// fs.readdir
type ReadDirRequest  struct { Path string; MaxEntries int }
type ReadDirResponse struct { Entries []DirEntry; Truncated bool }

// fs.samefs
type SameFSRequest  struct { Path1, Path2 string }
type SameFSResponse struct { Same bool }

// fs.mkdir
type MkdirRequest struct { Path string; Perm uint32 }

// fs.remove
type RemoveRequest  struct { Path string; Recursive bool; IgnorePaths []string }
type RemoveResponse struct { Removed bool; Disposition string; RemovedBytes int64 }

// tree.hardlink / tree.reflink
type TreeCreateRequest  struct { Plan hardlinktree.TreePlan; Mode string }
type TreeCreateResponse struct { Created int; SkippedExists int; RolledBack bool; Err string }
type TreeRemoveRequest  struct { Plan hardlinktree.TreePlan }

// control.cancel
type CancelRequest struct { RequestIDs []string }
```

### Error Codes

Stream errors (crash, disconnect): all in-flight ops fail with `connection_lost`, lazy reconnect on next call.

Op error codes: `path_not_allowed`, `path_not_found`, `permission_denied`, `cross_device`, `tree_partial_rollback`, `version_skew`, `request_too_large`, `internal`, `connection_lost`, `cancelled`, `deadline_exceeded`.

### Cancellation

qui sends `control.cancel` on stdin with the requestIDs to abort. Helper cancels the matching contexts. Ops exit cooperatively via `ctx.Done()` checks between syscalls. Tree ops enter rollback path on cancel.

### Per-Op Bounds

| Field | Cap | Rationale |
|---|---|---|
| StatRequest.Paths / LstatRequest.Paths | 1024 | Per-torrent file lists |
| WalkRequest.IgnorePaths | 1024 | Orphanscan ignore list |
| TreeCreateRequest.Plan.Files | 10,000 | Single cross-seed match |
| Any single path | 4,096 bytes | Linux PATH_MAX |
| Per-command line length | 16 MiB | Stdio buffer cap |

If a batch exceeds a cap, `fsops.Remote` chunks automatically and merges results.

## Path Safety

All enforcement is helper-side via `pkg/fsexec`:

- All paths must be absolute, clean, and under an allowed root (passed as `--root` flags at startup)
- Linux: `os.Root` (Go 1.24+) wraps allowed-root directories, uses `openat2(RESOLVE_BENEATH)` under the hood
- macOS/Windows: `os.Root` with userspace-resolution fallback + symlink rejection
- Destructive ops refuse to operate on the allowed-root itself (must be a strict descendant)
- `..` rejected at command validation time
- Device-ID guard: startup records `dev` of each allowed root, rejects ops whose resolved path is on a different device (catches unmounted roots)
- TOCTOU contract: `ResolveSafe()` returns an `*os.Root` handle; all subsequent ops use that same handle via `*at` syscalls

**Audit log:** `~/.local/state/qui-helper/audit.log` records every destructive op as JSON lines: `{ts, op, path, request_id, qui_session_id, outcome, error}`. In-process size-based rotation (50 MB, 5 archives).

## SSH Credentials & Deployment

### Credential Model

Per instance, the user provides:
- **Host** (defaults port 22)
- **Username**
- **Auth**: private key (OpenSSH format, optional passphrase) or password

All credential material encrypted at rest with `sessionSecret` (AES-GCM, same pattern as qBittorrent passwords). Host key captured on first connect (TOFU), verified on every subsequent connect.

### Deploy Flow

1. qui opens SSH connection
2. Detects remote arch: `uname -m && uname -s` -> maps to cross-compiled binary
3. Downloads matching binary from GitHub releases to qui host, verifies SHA256 against embedded constant
4. SCPs to `~/.config/qui-helper/qui-helper` (mode 0700, atomic via temp + rename)
5. Runs `qui-helper version --json` to confirm deployment
6. Caches version/capabilities/reflink-roots on instance record

Auto-redeploy on version mismatch at connect time.

### Hardening

Optional `authorized_keys` restriction to lock SSH key to helper-only access:
```
command="/home/user/.config/qui-helper/qui-helper serve --stdio --root /home/user/data --root /home/user/seed",no-port-forwarding,no-X11-forwarding,no-agent-forwarding ssh-ed25519 AAAA...
```

## Schema

Next available migration numbers: 077 (sqlite) / 078 (postgres).

New columns on `instances`:

```sql
ALTER TABLE instances ADD COLUMN ssh_host                      TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN ssh_port                      INTEGER NOT NULL DEFAULT 22;
ALTER TABLE instances ADD COLUMN ssh_username                  TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN ssh_auth_type                 TEXT NOT NULL DEFAULT '' CHECK (ssh_auth_type IN ('', 'key', 'password'));
ALTER TABLE instances ADD COLUMN ssh_key_encrypted             TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN ssh_key_passphrase_encrypted  TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN ssh_password_encrypted        TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN ssh_host_key                  TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN helper_path                   TEXT NOT NULL DEFAULT '~/.config/qui-helper/qui-helper';
ALTER TABLE instances ADD COLUMN helper_version                TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN helper_capabilities           TEXT NOT NULL DEFAULT '[]';
ALTER TABLE instances ADD COLUMN helper_allowed_roots          TEXT NOT NULL DEFAULT '[]';
ALTER TABLE instances ADD COLUMN helper_reflink_roots          TEXT NOT NULL DEFAULT '[]';
ALTER TABLE instances ADD COLUMN helper_platform               TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN helper_deployed_at            DATETIME;
ALTER TABLE instances ADD COLUMN helper_last_activity_at       DATETIME;
```

`HasFilesystemAccess(instance) -> (FilesystemMode, bool)` returns true if `has_local_filesystem_access || (ssh_host != '' && helper_deployed_at IS NOT NULL)`.

## Helper Binary

**Location:** `cmd/qui-helper/main.go`. Zero `internal/` imports (CI-enforced).

**Import graph:** `pkg/agent/proto`, `pkg/fsexec`, `pkg/hardlinktree`, `pkg/reflinktree`, `pkg/hardlink`, `pkg/fsutil`. Single-digit-MB binary.

**Subcommands:**
- `serve --stdio --root /path1 --root /path2` -- long-running mode, NDJSON loop
- `version --json` -- prints HelloBanner and exits
- `version` -- human-readable output

**Cross-compile:** linux/{amd64,arm64}, darwin/{amd64,arm64}. Windows/amd64 best-effort.

**No config file.** Everything via flags. Stateless. Cleanup: `rm -rf ~/.config/qui-helper/ ~/.local/state/qui-helper/`.

## SSH Pool

`internal/sshpool` manages per-instance SSH connections.

Key behaviors:
- Lazy connect on first FS op
- Concurrent ops multiplexed via NDJSON requestIDs on single stdio channel
- Pending results map with 5-min TTL
- Reconnect backoff: exponential 5s -> 60s with +/-20% jitter
- SSH auth failure: 60s pause, 3 sequential failures -> "credentials invalid" state
- Sweeper goroutine: connection health (30s), pending TTL (30s), reconnect scheduler (5s)
- `max_inflight_per_instance = 32`

## API Endpoints

```
POST   /api/instances/{id}/ssh-test         -- test SSH credentials, return host key fingerprint
POST   /api/instances/{id}/helper/deploy    -- push binary, return parsed HelloBanner
POST   /api/instances/{id}/helper/redeploy  -- re-push binary (upgrade)
DELETE /api/instances/{id}/helper           -- disconnect + remove binary + clear helper fields
DELETE /api/instances/{id}/ssh-credentials  -- clear SSH credential fields
GET    /api/instances/{id}/helper           -- return helper status (version, capabilities, etc.)
```

## Frontend

Instance form replaces `hasLocalFilesystemAccess` toggle with a RadioGroup:
- None / Local / Remote helper

"Remote helper" reveals SSH credential fields, test connection button (TOFU host key confirmation), deploy button with progress UI, and helper status card.

## Implementation Plan

### PR 1: Design doc + proto types

- `documentation/design/remote-helper.md` (this document)
- `pkg/agent/proto/proto.go` -- Command, Result, HelloBanner envelopes
- `pkg/agent/proto/ops.go` -- all op request/response types
- `pkg/agent/proto/proto_test.go` -- JSON round-trip tests

Leaf package, zero `internal/` imports.

### PR 2: Backend interface + local backend + pool

- `internal/fsops/backend.go` -- Backend interface (17 methods)
- `internal/fsops/types.go` -- value types (FileInfo, LstatInfo, WalkEntry, etc.)
- `internal/fsops/errors.go` -- sentinel errors
- `internal/fsops/noop.go` -- noop backend for instances without FS access
- `internal/fsops/pool.go` -- Pool resolver (instance ID -> Backend)
- `internal/fsops/local/` -- local backend implementation + platform-specific Statfs
- Tests for all of the above

No callsite changes yet. Services still use `os.*` directly.

### PR 3: Service callsite refactors

Migrate all `os.*`/`unix.*` callsites to `backend.*`. Zero behavioral change. Could split by service area if too large:

- `automations/missing_files.go` -- `os.Stat` -> `backend.Stat`
- `automations/free_space.go` -- `unix.Statfs` -> `backend.Statfs`
- `automations/hardlink_index.go` -- `os.Lstat` + FileID -> `backend.Lstat`
- `qbittorrent/delete_cleanup.go` -- `os.Stat`/`os.Remove` -> `backend.Stat`/`backend.Remove`
- `dirscan/fileid_index.go` -- FileID index via backend
- `dirscan/inject.go` + `crossseed/FindMatchingBaseDir` -- link-tree ops via backend
- `dirscan/scanner.go` -- `filepath.WalkDir` callback -> `backend.WalkDir` channel
- `orphanscan/walker.go` + `delete.go` -- full walker + delete via backend

All existing tests must pass with zero behavioral diff.

### PR 4: Schema + models

- Migration 077/078: 16 SSH/helper columns on `instances`
- `internal/models/instance.go` -- extend Instance struct with SSH/helper fields
- `internal/models/filesystem_access.go` -- `HasFilesystemAccess` helper
- `internal/models/filesystem_access_test.go`
- Update InstanceStore Create/Update to handle new fields

### PR 5: Path safety + SSH pool + helper binary

- `pkg/fsexec/` -- SafeRoot, Roots, ResolveSafe with os.Root wrapping, property tests
- `internal/sshpool/` -- pool, transport (TOFU), deploy, sweeper
- `cmd/qui-helper/` -- main.go, server (NDJSON loop, diag.echo), executor stub
- `Makefile` -- `make helper` target
- CI workflow for helper (import-graph guard, cross-compile)

### PR 6: API endpoints + wiring

- `internal/api/handlers/helper.go` -- 6 endpoints
- `internal/api/server.go` -- route registration
- Wire sshpool into Dependencies struct
- Wire fsops.Pool into services via `cmd/qui/main.go`

### PR 7: First real op (fs.statfs) end-to-end

- `internal/fsops/remote/remote.go` -- Remote backend backed by SSH pool
- Wire `sshpool.Pool.Submit`/`Cancel` with real SSH session dispatch
- Implement `fs.statfs` in helper executor
- Pool resolver returns Remote backend for helper-configured instances
- Dual-backend integration test

### PR 8+: Remaining ops (one PR each)

Read ops first, destructive ops later:

1. `fs.stat` -- missing-files condition
2. `fs.lstat` -- hardlink index, FileID index
3. `fs.samefs` -- hardlink/reflink precondition
4. `fs.readdir` -- disc-layout detection, orphanscan
5. `fs.walk` (streaming) -- dirscan, orphanscan
6. `fs.mkdir` -- link-tree materialization
7. `fs.remove` -- managed delete cleanup, orphanscan
8. `fs.removeall` -- orphanscan directory deletion
9. `tree.hardlink` -- cross-seed inject
10. `tree.reflink` -- cross-seed inject (capability-gated)
11. `tree.remove` -- link-tree rollback

Each PR adds: helper executor case + Remote backend method + dual-backend integration test.

### Frontend PR (parallel with PR 7+)

- Instance form RadioGroup (None / Local / Remote helper)
- SSH credential fields + host key confirmation modal
- Deploy button + progress UI
- Helper status card
- `authorized_keys` hardening snippet with copy button

## Salvageable Code from PR #1820

The following packages from the original `feat/fsops-backend-scaffold` branch are production-quality and can be cherry-picked onto fresh branches with minimal changes:

| Package | Verdict | Tests |
|---|---|---|
| `pkg/agent/proto` | Use as-is | 38 tests |
| `internal/fsops` (interface, pool, noop, types, errors) | Use as-is | 4 tests |
| `internal/fsops/local` | Use as-is | 35+ tests |
| `pkg/fsexec` | Use as-is | 17 tests |
| `cmd/qui-helper` | Use as-is | 5 tests |
| `internal/models/filesystem_access` | Use as-is | 6 tests |
| `internal/sshpool` | Needs test coverage for deploy.go and sweeper.go | 12 tests |

All packages have zero coupling to the unrelated changes in PR #1820 and will apply cleanly on develop.
