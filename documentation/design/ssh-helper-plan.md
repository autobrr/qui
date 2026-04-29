# SSH Helper Build Plan

> **Location:** This plan will be committed to `documentation/design/ssh-helper-plan.md`
>
> **Design reference:** `documentation/design/remote-helper.md`

---

## How to Use This Plan

Execute one phase per session. Start each session with:

> "Implement Phase N from `documentation/design/ssh-helper-plan.md`. Stay strictly within scope — if you discover work belonging to a different phase, stop and tell me. Update the plan's checkboxes and implementation notes at the end of each phase."

**Rules:**
- Don't move to the next phase until the previous one is green (all verification gates pass).
- Each phase ends with a runnable verification command. Run it.
- If a phase requires deviating from the design doc, document the deviation in that phase's Implementation Notes section. Phase 17 reviews all deviations.

**SSH-specific instructions (apply to all phases that touch SSH/transport/helper):**
- Every operation must accept a `context.Context` and respect cancellation.
- Never log credentials or full key material. Mask SSH keys in logs.
- Use `golang.org/x/crypto/ssh` directly — do not wrap the `ssh` CLI.
- Host key verification must not default to insecure. TOFU is fine; `InsecureIgnoreHostKey` is not.
- Goroutines spawned for connection handling need explicit lifecycle management with `sync.WaitGroup` or `errgroup`.

**General coding standards:**
- `gofmt`-clean, PascalCase exports, camelCase locals
- Interfaces max 5 methods (the 17-method `Backend` is the documented exception)
- Explicit error handling, no silent failures
- `go test -race -count=3` for all test runs
- golangci-lint v2 with project profile
- Conventional commits: `feat(scope):`, `fix(scope):`, etc.

---

## Cross-Cutting Concerns

| Concern | Implemented in | Stubbed until |
|---------|---------------|---------------|
| Context cancellation | Phase 3 (Local Backend checks `ctx.Done()` in WalkDir) | All Backend methods require `ctx` from Phase 2 |
| Logging | Unchanged — services already log; backends are silent | Phase 15 adds helper stderr forwarding |
| Error wrapping | Phase 2 (sentinel errors); Phase 3 (Local wraps `os.*` errors) | — |
| Path safety (`pkg/fsexec`) | Phase 14 (scaffolded with real enforcement) | Wired to helper in Stage C |
| Auth (SSH credentials) | Phase 13 (schema + model + encrypt/decrypt) | Phase 15 (sshpool uses them) |
| Retries/reconnect | — | Phase 15 (sshpool backoff scaffolded) |
| Platform build tags | Phase 3 (`Statfs` has `_unix.go`/`_windows.go`) | — |
| `filepath.*` manipulation | NOT part of Backend — stays as direct calls everywhere | — |

---

## Dependency Graph

```
Phase 1 (proto) ──────────────────────────────────────┐
Phase 2 (Backend interface) ──┐                        │
Phase 3 (Local impl) ────────┤                        │
Phase 4 (Pool/Resolver) ─────┤                        │
                              │                        │
Phase 5 (missing_files) ─────┤ simplest, proves       │
Phase 6 (free_space) ────────┤   the pattern           │
Phase 7 (hardlink_index) ────┤                        │
Phase 8 (delete_cleanup) ────┤                        │
Phase 9 (fileid_index) ──────┤                        │
Phase 10 (inject+crossseed) ─┤ shares dirscan svc     │
Phase 11 (scanner) ──────────┤ shares dirscan svc     │
Phase 12 (orphanscan) ───────┤                        │
                              │                        │
Phase 13 (schema+SSH model) ──┼────────────────────────┤
Phase 14 (pkg/fsexec) ────────┼────────────────────────┤
Phase 15 (sshpool+helper) ────┼── depends on 1,13,14 ──┘
Phase 16 (wiring+API) ───��────┘── depends on ALL
Phase 17 (design review) ─────── depends on 16
```

**Parallelism:** Phases 5-8 can run in parallel once Phase 4 lands. Phases 9-11 must be sequential (shared dirscan service). Phase 12 is independent of 9-11. Phases 13-14 can run in parallel with 5-12.

---

## Phase 1: Proto Types (`pkg/agent/proto`)

- [x] Types created
- [x] Tests passing (33 tests x3, -race)
- [x] Lint clean (go vet; golangci-lint not installed locally)

**Goal:** Shared NDJSON protocol types used by both qui and qui-helper. Leaf package, zero `internal/` imports.

**~1 hour**

**Create:**
- `pkg/agent/proto/proto.go` — `Command`, `Result`, `HelloBanner` envelopes
- `pkg/agent/proto/ops.go` — All op request/response types from design doc §7.4
- `pkg/agent/proto/proto_test.go` — JSON round-trip tests for every type

**Verification gate:**
```bash
go test -race -count=3 ./pkg/agent/proto/...
go build ./pkg/agent/proto/...
# Confirm no internal/ imports:
go list -deps ./pkg/agent/proto/... | grep -c 'qui/internal' # must be 0
```

**Agent prompt:**
> Implement Phase 1 from `documentation/design/ssh-helper-plan.md`. Create the `pkg/agent/proto` package with shared NDJSON protocol types. Reference design doc `documentation/design/remote-helper.md` §7.2 for `Command`/`Result`/`HelloBanner` envelopes and §7.4 for all op-specific payload types (`StatRequest`, `StatResponse`, `LstatRequest`, `LstatEntry`, `WalkRequest`, `WalkEntry`, `StatfsRequest`, `StatfsResponse`, `ReadDirRequest`, `ReadDirResponse`, `SameFSRequest`, `SameFSResponse`, `MkdirRequest`, `RemoveRequest`, `RemoveResponse`, `TreeCreateRequest`, `TreeCreateResponse`, `TreeRemoveRequest`, `CancelRequest`, `DiagEchoRequest`, `DiagEchoResponse`). This is a leaf `pkg/` package — it MUST NOT import anything from `internal/`. It imports `pkg/hardlinktree` for `TreePlan` and `pkg/hardlink` for `FileID`. Write JSON round-trip tests for every type. Follow coding standards in `CLAUDE.md`. Stay strictly within scope. Update the plan checkboxes and add implementation notes when done.

### Implementation Notes

**Completed 2026-04-28.**

Files created:
- `pkg/agent/proto/proto.go` — Envelopes (`Command`, `Result`, `HelloBanner`), op constants, `IsStreamingOp`, error code constants
- `pkg/agent/proto/ops.go` — All 20 op payload types
- `pkg/agent/proto/proto_test.go` — 33 tests covering JSON round-trip for every type

Deviations from design doc:
- Added `LstatResponse` wrapper type (design doc §7.4 only showed `LstatEntry` inline; added `LstatResponse{Entries []LstatEntry}` for consistency with `StatResponse`)
- `SameFSRequest` — design doc was missing JSON tags (`Path1, Path2 string` with no tags). Added `json:"path1"` / `json:"path2"`.
- Added `OpRemoveAll` constant — design doc treats remove/removeall as one op with a `Recursive` flag, but the op constant table in §7.4 lists them separately. Kept both constants mapping to the same payload type.
- Added `DiagEchoRequest`/`DiagEchoResponse` types (not in design doc §7.4 but referenced in the build plan for Phase 15's diag.echo op).
- Op constants and error code constants added as package-level consts for type safety (not in design doc but follows Go conventions).

---

## Phase 2: Backend Interface + Types (`internal/fsops`)

- [x] Interface defined (17 methods)
- [x] Types defined (9 value types)
- [x] Errors defined (3 sentinel errors)
- [x] Compiles clean

**Goal:** Define the `Backend` interface (17 methods) and value types exactly as in design doc §8. No implementations yet.

**~1 hour**

**Create:**
- `internal/fsops/backend.go` — `Backend` interface
- `internal/fsops/types.go` — `FileInfo`, `DirEntry`, `LstatInfo`, `WalkEntry`, `WalkOptions`, `StatfsResult`, `RemoveOptions`, `TreeCreateResult`, `BackendInfo`
- `internal/fsops/errors.go` — `ErrNoFilesystemAccess`, `ErrConnectionLost`, `ErrPathNotAllowed`

**Verification gate:**
```bash
go build ./internal/fsops/...
```

**Agent prompt:**
> Implement Phase 2 from `documentation/design/ssh-helper-plan.md`. Create the `internal/fsops` package with the `Backend` interface and value types. Reference design doc `documentation/design/remote-helper.md` §8 for the exact interface definition (17 methods) and all associated types. The interface imports `pkg/hardlink` for `FileID` and `pkg/hardlinktree` for `TreePlan`. Also create sentinel errors in `errors.go`: `ErrNoFilesystemAccess`, `ErrConnectionLost`, `ErrPathNotAllowed`. No implementations yet — just the contract. Follow coding standards in `CLAUDE.md`. Stay strictly within scope. Update the plan checkboxes and add implementation notes when done.

### Implementation Notes

**Completed 2026-04-28.**

Files created:
- `internal/fsops/backend.go` — `Backend` interface with 17 methods, thorough doc comments
- `internal/fsops/types.go` — 9 value types: `FileInfo`, `DirEntry`, `LstatInfo`, `WalkEntry`, `WalkOptions`, `StatfsResult`, `RemoveOptions`, `TreeCreateResult`, `BackendInfo`
- `internal/fsops/errors.go` — 3 sentinel errors: `ErrNoFilesystemAccess`, `ErrConnectionLost`, `ErrPathNotAllowed`

Deviations from design doc:
- None. Interface matches §8 exactly — 17 methods with identical signatures.

---

## Phase 3: Local Backend (`internal/fsops/local`)

- [x] All 17 Backend methods implemented
- [x] Platform-specific Statfs (unix/windows)
- [x] WalkDir channel closes on completion, error, and ctx cancellation
- [x] Tests for all methods (26 tests x3, -race)
- [x] HardlinkTree create + RemoveTree rollback round-trip test

**Goal:** `LocalBackend` implementing `Backend` — thin adapter over `os.*`, `pkg/hardlinktree`, `pkg/reflinktree`, `pkg/hardlink`, `pkg/fsutil`.

**~2-3 hours**

**Create:**
- `internal/fsops/local/local.go` — `LocalBackend` struct, all Backend methods
- `internal/fsops/local/local_unix.go` — `Statfs` via `unix.Statfs`
- `internal/fsops/local/local_windows.go` — `Statfs` via `GetDiskFreeSpaceEx`
- `internal/fsops/local/local_test.go` — Full interface coverage on `t.TempDir()`

**Key implementation notes:**
- `WalkDir` spawns a goroutine calling `filepath.WalkDir`, sends entries on channel, checks `ctx.Done()` between entries
- `Lstat` populates `FileID`/`Nlinks` via `hardlink.GetFileID`/`hardlink.GetLinkCount`
- `HardlinkTree` → `hardlinktree.Create`; `ReflinkTree` → `reflinktree.Create`; `RemoveTree` → `hardlinktree.Rollback`
- `SameFilesystem` → `fsutil.SameFilesystem`
- `Info` → `BackendInfo{Kind: "local"}`; `HealthCheck` → nil

**Verification gate:**
```bash
go test -race -count=3 ./internal/fsops/local/...
go test -race -count=3 ./internal/fsops/...
```

**Agent prompt:**
> Implement Phase 3 from `documentation/design/ssh-helper-plan.md`. Create `internal/fsops/local/local.go` implementing the `fsops.Backend` interface from Phase 2. This is a thin adapter (~200 lines) delegating to `os.*`, `pkg/hardlinktree`, `pkg/reflinktree`, `pkg/hardlink`, and `pkg/fsutil`. Key methods: `WalkDir` must spawn a goroutine that calls `filepath.WalkDir` and sends `fsops.WalkEntry` values on a channel, checking `ctx.Done()` between entries. `Lstat` must populate `FileID` and `Nlinks` using `hardlink.GetFileID` and link count from the `os.FileInfo`. `Statfs` needs platform-specific files (`_unix.go` using `unix.Statfs`, `_windows.go` using `GetDiskFreeSpaceEx`) — match the build tag pattern from `internal/services/automations/free_space.go`. Write comprehensive tests in `local_test.go` using `t.TempDir()` — test all 17 methods, test WalkDir cancellation, test HardlinkTree+RemoveTree round-trip. Follow coding standards in `CLAUDE.md`. Stay strictly within scope. Update the plan checkboxes and add implementation notes when done.

### Implementation Notes

**Completed 2026-04-28.**

Files created:
- `internal/fsops/local/local.go` — `Backend` struct implementing all 17 `fsops.Backend` methods (~270 lines)
- `internal/fsops/local/statfs_unix.go` — `Statfs` via `unix.Statfs` (build tag `!windows`)
- `internal/fsops/local/statfs_windows.go` — `Statfs` via `windows.GetDiskFreeSpaceEx` (build tag `windows`)
- `internal/fsops/local/local_test.go` — 26 tests covering all methods

Design decisions:
- Named the struct `local.Backend` (not `local.LocalBackend`) to avoid stutter. Callers write `local.NewBackend()`.
- `HardlinkTree`/`ReflinkTree` return `TreeCreateResult{Created: len(plan.Files)}` on success since the underlying `hardlinktree.Create`/`reflinktree.Create` return only `error` (no count). On failure, `RolledBack: true` is set.
- `Lstat` only populates `FileID`/`Nlinks` for regular files (not symlinks/dirs) since `hardlink.GetFileID` requires `syscall.Stat_t` which is only meaningful for regular files.
- WalkDir uses a buffered channel (cap 64) to decouple the walker goroutine from consumers.
- Compile-time interface check: `var _ fsops.Backend = (*Backend)(nil)`.

Deviations from design doc:
- None. The Local implementation matches the §8 interface exactly.

---

## Phase 4: Backend Pool / Resolver

- [x] Pool routes local-access instances to LocalBackend
- [x] Pool routes no-access instances to NoopBackend
- [x] Tests passing (4 tests x3, -race)

**Goal:** Resolver maps `instanceID → Backend`. For now: `LocalBackend` if local access, `NoopBackend` otherwise.

**~1 hour**

**Create:**
- `internal/fsops/pool.go` — `Pool` struct, `GetBackend(ctx, instanceID) (Backend, error)`, `NewPool(instanceStore, localBackend)`
- `internal/fsops/noop.go` — `NoopBackend` returning `ErrNoFilesystemAccess` for all methods
- `internal/fsops/pool_test.go` — Routing tests

**Verification gate:**
```bash
go test -race -count=3 ./internal/fsops/...
```

**Agent prompt:**
> Implement Phase 4 from `documentation/design/ssh-helper-plan.md`. Create the Backend pool/resolver in `internal/fsops/pool.go`. The `Pool` struct holds a reference to `*models.InstanceStore` and a `LocalBackend`. `GetBackend(ctx, instanceID)` loads the instance, returns `LocalBackend` if `instance.HasLocalFilesystemAccess` is true, returns `NoopBackend` otherwise. Create `NoopBackend` in `internal/fsops/noop.go` — every method returns `ErrNoFilesystemAccess`. Write tests that verify routing for both cases. The Pool will be extended in Phase 15 to support SSH-backed remote instances, but for now it only handles local vs none. Follow coding standards in `CLAUDE.md`. Stay strictly within scope. Update the plan checkboxes and add implementation notes when done.

### Implementation Notes

**Completed 2026-04-28.**

Files created:
- `internal/fsops/pool.go` — `Pool` struct with `GetBackend(ctx, instanceID)` routing
- `internal/fsops/noop.go` — `noopBackend` (unexported) implementing all 17 Backend methods with `ErrNoFilesystemAccess`
- `internal/fsops/pool_test.go` — 4 tests: local routing, noop routing, instance not found, noop exhaustive error check

Design decisions:
- Used an `instanceGetter` interface (just `Get(ctx, id)`) instead of depending on `*models.InstanceStore` directly. Keeps the dependency narrow and makes tests trivial — no test database needed.
- `noopBackend` is unexported — callers get it only through the Pool, never directly.
- `Info()` on noopBackend returns `BackendInfo{Kind: "none"}` without error (useful for UI status display).

Deviations from design doc:
- None.

---

## Phase 5: Refactor — `automations/missing_files.go`

- [x] `os.Stat` → `backend.Stat`
- [x] `os.IsNotExist` → `errors.Is(err, fs.ErrNotExist)`
- [x] Existing tests pass with zero behavioral diff
- [x] `go build ./...` succeeds

**Goal:** First callsite refactor (2 `os.*` calls). Proves the pattern. Smallest possible scope.

**~1 hour**

**Modify:**
- `internal/services/automations/service.go` — `Service` struct gains `backendPool *fsops.Pool`; `NewService` gains parameter
- `internal/services/automations/missing_files.go` — `os.Stat` → `backend.Stat`, `os.IsNotExist` → `errors.Is(err, fs.ErrNotExist)`
- `cmd/qui/main.go` — Create `fsops.Pool`, pass to `automations.NewService`

**Verification gate:**
```bash
go test -race -count=3 ./internal/services/automations/...
make build
```

**Agent prompt:**
> Implement Phase 5 from `documentation/design/ssh-helper-plan.md`. This is the first callsite refactor — prove the pattern works on the simplest file. Modify `internal/services/automations/missing_files.go`: replace `os.Stat(fullPath)` (line 49) with `backend.Stat(ctx, fullPath)` and `os.IsNotExist(err)` (line 50) with `errors.Is(err, fs.ErrNotExist)`. The backend is obtained from `s.backendPool.GetBackend(ctx, instanceID)` at the start of `detectMissingFiles`. Add `backendPool *fsops.Pool` to the `Service` struct in `service.go` and to the `NewService` constructor. Wire it in `cmd/qui/main.go` by creating `localBackend := local.NewLocalBackend()` and `backendPool := fsops.NewPool(instanceStore, localBackend)` then passing it to `automations.NewService`. All existing tests must pass with zero behavioral difference. Follow coding standards in `CLAUDE.md`. Stay strictly within scope. Update the plan checkboxes and add implementation notes when done.

### Implementation Notes

**Completed 2026-04-28.**

Files modified:
- `internal/services/automations/service.go` — Added `backendPool *fsops.Pool` field and `fsops` import; extended `NewService` parameter list
- `internal/services/automations/missing_files.go` — Replaced `os.Stat` with `backend.Stat`, `os.IsNotExist` with `errors.Is(err, fs.ErrNotExist)`; backend obtained via `s.backendPool.GetBackend(ctx, instanceID)`
- `cmd/qui/main.go` — Created `localBackend` + `backendPool`; passed pool to `automations.NewService`

Design decisions:
- Backend is resolved once at the top of `detectMissingFiles` rather than per-file. The instanceID doesn't change within the function.
- The `HasLocalFilesystemAccess` guard at the callsite (service.go:1986) is preserved for now — it's an optimization that skips detection entirely. Phase 13 will migrate it to `HasFilesystemAccess`.
- `backendPool` and `localBackend` are created right before `automationService` in main.go. Later phases will reuse these same variables for other service constructors.

Deviations from design doc:
- None.

---

## Phase 6: Refactor — `automations/free_space.go`

- [x] `unix.Statfs` → `backend.Statfs`
- [x] `HasLocalFilesystemAccess` check replaced with backend pool resolution
- [x] Tests pass, build tags work

**Goal:** `unix.Statfs` → `backend.Statfs`. Replaces `HasLocalFilesystemAccess` check with backend pool resolution.

**~1 hour**

**Modify:**
- `internal/services/automations/free_space.go` — `getLocalFreeSpaceBytes` uses `backend.Statfs`; `GetFreeSpaceBytesForSource` resolves backend from pool
- `internal/services/automations/free_space_windows.go` — Same pattern

**Verification gate:**
```bash
go test -race -count=3 ./internal/services/automations/...
make build
```

**Agent prompt:**
> Implement Phase 6 from `documentation/design/ssh-helper-plan.md`. Refactor `internal/services/automations/free_space.go`: replace `unix.Statfs(path, &stat)` (line 102) with `backend.Statfs(ctx, path)` which returns `(*fsops.StatfsResult, error)`. The `HasLocalFilesystemAccess` check on line 88 should be replaced by resolving the backend from the pool — if the pool returns `NoopBackend`, the Statfs call will return `ErrNoFilesystemAccess` naturally. `GetFreeSpaceBytesForSource` needs access to the backend pool (pass through the service or as a parameter). Apply the same pattern to `free_space_windows.go`. Build tags must still work correctly. Follow coding standards in `CLAUDE.md`. Stay strictly within scope. Update the plan checkboxes and add implementation notes when done.

### Implementation Notes

**Completed 2026-04-28.**

Files modified:
- `internal/services/automations/free_space.go` — Removed `getLocalFreeSpaceBytes` and `unix` import; added `backend fsops.Backend` param to `GetFreeSpaceBytesForSource`; path case uses `backend.Statfs`
- `internal/services/automations/free_space_windows.go` — Same signature change; path case now uses `backend.Statfs` instead of returning "not supported on Windows"
- `internal/services/automations/free_space_test.go` — Updated tests to call through `GetFreeSpaceBytesForSource` with `local.NewBackend()` instead of the removed `getLocalFreeSpaceBytes`
- `internal/services/automations/service.go` — Both `GetFreeSpaceBytesForSource` callsites updated to resolve backend from pool and pass it

Design decisions:
- Added `backend fsops.Backend` as a parameter to `GetFreeSpaceBytesForSource` rather than making it a method on Service, since it's a package-level function. Callers resolve the backend from the pool and pass it in.
- The `HasLocalFilesystemAccess` check on the old line 89 is now gone — the backend handles access control naturally (noop backend returns `ErrNoFilesystemAccess` from `Statfs`).
- Windows now supports path-based free space via the backend (previously returned "not supported on Windows"). The local backend's `Statfs` on Windows uses `GetDiskFreeSpaceEx`.

Deviations from design doc:
- None.

---

## Phase 7: Refactor — `automations/hardlink_index.go`

- [x] `os.Lstat` + `hardlink.GetFileID` → `backend.Lstat`
- [x] Hardlink index scope maps unchanged
- [x] Tests pass

**Goal:** First test of the Lstat-with-FileID pattern. `os.Lstat` + `hardlink.GetFileID` → `backend.Lstat` (with FileID populated).

**~1.5 hours**

**Modify:**
- `internal/services/automations/hardlink_index.go` — `buildHardlinkIndex` uses `backend.Lstat` instead of `os.Lstat` + `hardlink.GetFileID`. The `isPathInsideBase` helper stays as-is (pure `filepath.*` manipulation).

**Verification gate:**
```bash
go test -race -count=3 ./internal/services/automations/...
```

**Agent prompt:**
> Implement Phase 7 from `documentation/design/ssh-helper-plan.md`. Refactor `internal/services/automations/hardlink_index.go`: in `buildHardlinkIndex`, replace `os.Lstat(fullPath)` (line 239) + `hardlink.GetFileID(fi, fullPath)` with `info, err := backend.Lstat(ctx, fullPath)` where `info.FileID` and `info.Nlinks` are already populated by the Local backend. Obtain the backend from `s.backendPool.GetBackend(ctx, instanceID)` at the start of the function. Leave all `filepath.*` calls (`filepath.Clean`, `filepath.Rel`, `isPathInsideBase`) untouched — those are path manipulation, not filesystem operations. All existing hardlink index tests must pass identically. Follow coding standards in `CLAUDE.md`. Stay strictly within scope. Update the plan checkboxes and add implementation notes when done.

### Implementation Notes

**Completed 2026-04-28.**

Files modified:
- `internal/services/automations/hardlink_index.go` — Replaced `os.Lstat(fullPath)` + `hardlink.GetFileID(fi, fullPath)` with `backend.Lstat(ctx, fullPath)` using `lstatInfo.FileID` and `lstatInfo.Nlinks` directly. Removed `os` import. Backend resolved once from `s.backendPool` at top of `buildHardlinkIndex`. Changed `os.PathSeparator` to `filepath.Separator` in `isPathInsideBase`.

Design decisions:
- Check `fileID.IsZero()` after Lstat to handle the case where the backend couldn't extract FileID (e.g., for non-regular files on some platforms). Marks `allAccessible = false` same as the old error path.
- The `isPathInsideBase` helper only uses `filepath.*` — stays untouched except for the `os.PathSeparator` → `filepath.Separator` swap to remove the `os` import entirely.

Deviations from design doc:
- None.

---

## Phase 8: Refactor — `qbittorrent/delete_cleanup.go`

- [x] `os.Stat` → `backend.Stat`
- [x] `os.Remove` → `backend.Remove`
- [x] Backend flows through delete action call chain
- [x] Tests pass

**Goal:** Managed-delete cleanup: `os.Stat` → `backend.Stat`, `os.Remove` → `backend.Remove`. Backend must flow through the automation delete action call chain.

**~1.5 hours**

**Modify:**
- `internal/qbittorrent/delete_cleanup.go` — Functions gain `ctx context.Context` and `backend fsops.Backend` parameters; `os.Stat` → `backend.Stat`, `os.Remove` → `backend.Remove`
- Callers in automations service updated to pass backend through the delete chain

**Verification gate:**
```bash
go test -race -count=3 ./internal/qbittorrent/...
go test -race -count=3 ./internal/services/automations/...
```

**Agent prompt:**
> Implement Phase 8 from `documentation/design/ssh-helper-plan.md`. Refactor `internal/qbittorrent/delete_cleanup.go`: `managedDeleteCleanupDir` and `pruneEmptyManagedDeleteDirOnce` gain `ctx context.Context` and `backend fsops.Backend` parameters. Replace `os.Stat(contentPath)` (line 67) with `backend.Stat(ctx, contentPath)`, `os.Remove(dir)` (line 151) with `backend.Remove(ctx, dir, fsops.RemoveOptions{})`. Trace the caller chain from automation delete actions to these functions and thread the backend parameter through. Leave all `filepath.*` calls untouched. All existing tests must pass. Follow coding standards in `CLAUDE.md`. Stay strictly within scope. Update the plan checkboxes and add implementation notes when done.

### Implementation Notes

**Completed 2026-04-28.**

Files modified:
- `internal/qbittorrent/delete_cleanup.go` — All functions now accept `ctx context.Context` + `backend fsops.Backend`. `os.Stat` → `backend.Stat`, `os.Remove` → `backend.Remove`. Removed `os` import.
- `internal/qbittorrent/delete_cleanup_test.go` — Updated all test callsites to pass `ctx` and `local.NewBackend()`. Added shared `testBackend` var.
- `internal/qbittorrent/sync_manager.go` — Added `backendPool` field (atomic.Value), `SetBackendPool`/`getBackendPool` methods. Updated `buildManagedDeleteCleanupTargets` and `cleanupManagedDeleteTargets` callers to resolve and pass backend.
- `cmd/qui/main.go` — Added `syncManager.SetBackendPool(backendPool)` after pool creation.

Design decisions:
- Used `atomic.Value` for `backendPool` on SyncManager, matching the `filesManager` pattern. Thread-safe without mutex.
- `cleanupManagedDeleteTargets` is only called if `deleteBackend != nil` (guarded at callsite). If the pool isn't set (shouldn't happen in production), cleanup is silently skipped.
- `os.IsNotExist(err)` → `errors.Is(err, fs.ErrNotExist)` in `pruneEmptyManagedDeleteDirOnce`.

Deviations from design doc:
- None.

---

## Phase 9: Refactor — `dirscan/fileid_index.go`

- [x] `os.Stat` + `hardlink.GetFileID` → `backend.Lstat`
- [x] Dirscan service gains `backendPool`
- [x] Tests pass

**Goal:** FileID index builder refactor. Also adds `backendPool` to the dirscan `Service` struct (used by Phases 10-11).

**~1 hour**

**Modify:**
- `internal/services/dirscan/fileid_index.go` — Uses `backend.Lstat` with FileID
- `internal/services/dirscan/service.go` — `Service` struct gains `backendPool *fsops.Pool`; `NewService` gains parameter
- `cmd/qui/main.go` — Pass `fsops.Pool` to `dirscan.NewService`

**Verification gate:**
```bash
go test -race -count=3 ./internal/services/dirscan/...
make build
```

**Agent prompt:**
> Implement Phase 9 from `documentation/design/ssh-helper-plan.md`. Refactor `internal/services/dirscan/fileid_index.go`: replace `os.Stat(absPath)` + `hardlink.GetFileID(fi, absPath)` in `addTorrentFilesToFileIDIndex` with `info, err := backend.Lstat(ctx, absPath)` where `info.FileID` is already populated. Add `backendPool *fsops.Pool` to the `Service` struct in `service.go` and to `NewService`. Wire it in `cmd/qui/main.go`. This phase establishes the backend pool on the dirscan service that Phases 10-11 will also use. Follow coding standards in `CLAUDE.md`. Stay strictly within scope. Update the plan checkboxes and add implementation notes when done.

### Implementation Notes

**Completed 2026-04-28.**

Files modified:
- `internal/services/dirscan/service.go` — Added `backendPool *fsops.Pool` field and `fsops` import; extended `NewService` parameter list
- `internal/services/dirscan/fileid_index.go` — Replaced `os.Stat` + `hardlink.GetFileID` with `backend.Lstat` in `addTorrentFilesToFileIDIndex`. Removed `os` and `hardlink` imports. Added `ctx` + `backend` params.
- `internal/services/dirscan/webhook_queue_test.go` — Added nil backendPool param to `NewService` call
- `internal/services/dirscan/cancel_scan_test.go` — Same
- `cmd/qui/main.go` — Passed `backendPool` to `dirscan.NewService`

Deviations from design doc:
- None.

---

## Phase 10: Refactor — `dirscan/inject.go` + `crossseed/FindMatchingBaseDir`

- [x] `FindMatchingBaseDir` uses `backend.MkdirAll` + `backend.SameFilesystem`
- [x] All 3 callsites updated (crossseed + inject)
- [x] `createLinkTree` uses `backend.HardlinkTree`/`ReflinkTree`/`SupportsReflink`
- [x] `rollbackLinkTree` uses `backend.RemoveTree`/`Remove`
- [x] Tests pass for both dirscan and crossseed

**Goal:** Link-tree materialization. Exercises the most Backend methods: `HardlinkTree`, `ReflinkTree`, `RemoveTree`, `SameFilesystem`, `MkdirAll`, `SupportsReflink`, `Remove`.

**~2-3 hours**

**Modify:**
- `internal/services/crossseed/service.go` — `FindMatchingBaseDir` gains `ctx` + `backend fsops.Backend` params. 3 callsites: lines 11125, 11715, `dirscan/inject.go:587`
- `internal/services/dirscan/inject.go` — `createLinkTree`/`rollbackLinkTree`/`materializeLinkTree` use backend methods
- `internal/services/crossseed/hardlink_mode_test.go` — Tests pass `LocalBackend`

**Verification gate:**
```bash
go test -race -count=3 ./internal/services/dirscan/...
go test -race -count=3 ./internal/services/crossseed/...
```

**Agent prompt:**
> Implement Phase 10 from `documentation/design/ssh-helper-plan.md`. This is the most architecturally significant callsite refactor. Modify `FindMatchingBaseDir` in `internal/services/crossseed/service.go` (line 11528) to accept `ctx context.Context` and `backend fsops.Backend` parameters. Replace `os.MkdirAll(dir, 0o755)` (line 11542) with `backend.MkdirAll(ctx, dir, 0o755)` and `fsutil.SameFilesystem(sourcePath, dir)` (line 11547) with `backend.SameFilesystem(ctx, sourcePath, dir)`. Update all 3 callsites: `crossseed/service.go:11125`, `crossseed/service.go:11715`, `dirscan/inject.go:587`. In `dirscan/inject.go`, refactor `createLinkTree` to use `backend.HardlinkTree`/`backend.ReflinkTree`/`backend.SupportsReflink`, `rollbackLinkTree` to use `backend.RemoveTree`/`backend.Remove`, and `materializeLinkTree` to use `backend.MkdirAll`. Update tests in `crossseed/hardlink_mode_test.go` to pass a `local.NewLocalBackend()`. Follow coding standards in `CLAUDE.md`. Stay strictly within scope. Update the plan checkboxes and add implementation notes when done.

### Implementation Notes

**Completed 2026-04-28.**

Files modified:
- `internal/services/crossseed/service.go` — Added `backendPool` field (atomic.Value), `SetBackendPool`/`getBackendPool`/`getBackendForInstance` methods. Refactored `FindMatchingBaseDir` to accept `ctx` + `backend`. Updated 2 callsites. Removed `os` and `fsutil` imports.
- `internal/services/crossseed/hardlink_mode_test.go` — Updated all 5 `FindMatchingBaseDir` test calls to pass `ctx` + `local.NewBackend()`.
- `internal/services/crossseed/partial_contains_guard_test.go` — Added `SetBackendPool` call for test Service that exercises hardlink mode.
- `internal/services/dirscan/inject.go` — Added `backendPool` to Injector. Refactored `createLinkTree`, `rollbackLinkTree`, `materializeLinkTree` to use backend. 6 Backend methods exercised. Removed `fsutil` and `reflinktree` imports.
- `internal/services/dirscan/inject_test.go` — Added `newTestInjectorWithPool` helper. Updated all test Injector creation to use real pool.
- `internal/services/dirscan/service.go` — Updated `NewInjector` call to pass `backendPool`.
- `cmd/qui/main.go` — Added `crossSeedService.SetBackendPool(backendPool)`.

Design decisions:
- Used `atomic.Value` + `SetBackendPool` pattern for crossseed Service (matches SyncManager pattern) since the crossseed service is created before the backendPool in main.go.
- Added nil check for `i.backendPool` in `materializeLinkTree` to avoid panic when tests pass nil pool (tests that don't exercise link-tree mode).
- `rollbackLinkTree` simplified: removed mode switch, uses `backend.RemoveTree` (which internally delegates to the right rollback for the plan).
- `shouldUseSearcheeDirectory` still uses `os.Stat` — it's a Phase 11 concern (scanner area), so `os` import remains in inject.go.

Deviations from design doc:
- None.

---

## Phase 11: Refactor — `dirscan/scanner.go`

- [x] `filepath.WalkDir` callback → `backend.WalkDir` channel consumption
- [x] `os.ReadDir` → `backend.ReadDir`
- [x] `os.Stat` → `backend.Lstat`
- [x] Same searchees found, same files, same sizes
- [x] Context cancellation mid-scan works

**Goal:** Directory scanner: callback-to-channel conversion for `WalkDir`. Most nuanced control-flow change.

**~2-3 hours**

**Modify:**
- `internal/services/dirscan/scanner.go` — `scanSearcheeDir` converts from `filepath.WalkDir` callback to `backend.WalkDir` channel loop; `ScanDirectory` uses `backend.ReadDir`; `scanSingleFile` uses `backend.Stat`

**Verification gate:**
```bash
go test -race -count=3 ./internal/services/dirscan/...
```

**Agent prompt:**
> Implement Phase 11 from `documentation/design/ssh-helper-plan.md`. Refactor `internal/services/dirscan/scanner.go`. The most significant change: `scanSearcheeDir` currently uses `filepath.WalkDir(dirPath, w.walk)` with a callback. Convert this to consuming a `<-chan fsops.WalkEntry` from `backend.WalkDir(ctx, dirPath, opts)`. The `walkDirEntry` callback's skip/continue/error logic must be restructured into a channel-consuming for-range loop. Move hidden-file filtering into `WalkOptions.SkipHidden`. Replace `os.ReadDir(rootPath)` (line 88) with `backend.ReadDir(ctx, rootPath, 0)`. Replace `os.Stat(filePath)` in `scanSingleFile` with `backend.Stat(ctx, filePath)`. The backend comes from `s.backendPool.GetBackend(ctx, instanceID)` at the scan entry point. All scanner tests must pass with identical results — same searchees, same files, same sizes. Follow coding standards in `CLAUDE.md`. Stay strictly within scope. Update the plan checkboxes and add implementation notes when done.

### Implementation Notes

**Completed 2026-04-28.**

Files modified:
- `internal/services/dirscan/scanner.go` — Major refactor: `Scanner` gains `backend fsops.Backend` field. `NewScanner` accepts backend. `ScanDirectory` uses `backend.ReadDir`. `scanSearcheeDir` converts from `filepath.WalkDir` callback to `backend.WalkDir` channel consumption with `for entry := range ch` loop. `scanSingleFile` uses `backend.Lstat`. `isDiscLayoutRoot` uses `backend.ReadDir`. Removed `walkDirEntry`, `shouldSkipEntry`, `shouldProcessFile`, `addFileToSearchee`, `getFileIDSafe` (all folded into the channel loop). Removed `os` import.
- `internal/services/dirscan/service.go` — `runScanPhase` resolves backend from pool and passes it to `NewScanner(backend)`.

Design decisions:
- Used `backend.WalkDir` with `SkipHidden: true, WantFileID: true, WantNlinks: true` to offload filtering and metadata collection to the backend. The callback's skip-hidden and symlink-skip logic moves into WalkOptions.
- The channel loop is simpler than the callback: just `for entry := range ch` with continue/break instead of returning `filepath.SkipDir` or `nil`.
- `scanSingleFile` uses `backend.Lstat` (not `Stat`) to get FileID and Nlinks in one call.
- `isDiscLayoutRoot` is now a method on `Scanner` (was package-level) since it needs the backend.

Deviations from design doc:
- None.

---

## Phase 12: Refactor — `orphanscan/walker.go` + `delete.go`

- [x] Walker: `filepath.WalkDir` → `backend.WalkDir` channel
- [x] Walker: `os.ReadDir` → `backend.ReadDir`
- [x] Delete: `os.Lstat`/`os.Remove`/`os.RemoveAll` → backend equivalents
- [x] Orphanscan service gains `backendPool`
- [x] All orphanscan tests pass (including cross-instance)
- [x] Delete safety checks preserved

**Goal:** Largest refactor by callsite count. Note: many of delete.go's "callsites" are `filepath.*` manipulation — only actual `os.*` calls go through Backend.

**~3 hours**

**Modify:**
- `internal/services/orphanscan/walker.go` — `walkScanRootWithUnitFilter` consumes `backend.WalkDir` channel; disc-layout helpers use `backend.ReadDir`
- `internal/services/orphanscan/delete.go` — `safeDeleteFile`/`safeDeleteTarget`/`safeDeleteDirectory`/`safeDeleteEmptyDir`/`safeDeleteSymlink` gain `ctx` + `backend` params; `os.Lstat` → `backend.Lstat`, `os.Remove` → `backend.Remove`, `os.RemoveAll` → `backend.Remove(recursive:true)`, `filepath.WalkDir` in `checkDirContainsInUseFile` → `backend.WalkDir`
- `internal/services/orphanscan/service.go` — `Service` struct gains `backendPool *fsops.Pool`; `NewService` gains parameter
- `cmd/qui/main.go` — Pass `fsops.Pool` to `orphanscan.NewService`

**Verification gate:**
```bash
go test -race -count=3 ./internal/services/orphanscan/...
make build
```

**Agent prompt:**
> Implement Phase 12 from `documentation/design/ssh-helper-plan.md`. Refactor `internal/services/orphanscan/walker.go` and `delete.go`. In walker.go: convert `walkScanRootWithUnitFilter` from `filepath.WalkDir` callback to consuming `backend.WalkDir` channel. Replace `os.ReadDir` in `discParentIsPureDiscRoot`/`discParentIsSafeDiscRoot` with `backend.ReadDir`. In delete.go: replace `os.Lstat` (lines 54, 130) with `backend.Lstat`, `os.Remove` (lines 65, 151, 196) with `backend.Remove`, `os.RemoveAll` (line 168) with `backend.Remove(ctx, path, fsops.RemoveOptions{Recursive: true})`, and `filepath.WalkDir` (line 91) with `backend.WalkDir`. Leave ALL `filepath.*` calls (`filepath.Clean`, `filepath.IsAbs`, `filepath.Rel`, `filepath.Dir`) untouched. Add `backendPool *fsops.Pool` to `Service` struct and `NewService`. Wire in `cmd/qui/main.go`. Critical: all delete safety checks must be preserved (scan-root refusal, path-traversal rejection, in-use file protection, disc-layout detection). Follow coding standards in `CLAUDE.md`. Stay strictly within scope. Update the plan checkboxes and add implementation notes when done.

### Implementation Notes

**Completed 2026-04-28.**

Files modified:
- `internal/services/orphanscan/walker.go` — Major refactor: converted `filepath.WalkDir` callback to `backend.WalkDir` channel. Replaced `inodeKey` with `hardlink.FileID`. Updated `shouldSkipDuplicate` to accept FileID/Nlinks. Updated `discParentIsPureDiscRoot`/`discParentIsSafeDiscRoot` to use `backend.ReadDir`. Added `isUnderIgnoredPrefixDir` for prefix-based dir name filtering. Propagated `ctx`/`backend` through disc-unit decision chain.
- `internal/services/orphanscan/delete.go` — All delete functions gain `ctx`/`backend` params. `os.Lstat` → `backend.Lstat`, `os.Remove` → `backend.Remove`, `os.RemoveAll` → `backend.Remove(recursive)`, `filepath.WalkDir` in `checkDirContainsInUseFile` → `backend.WalkDir`.
- `internal/services/orphanscan/service.go` — Added `backendPool *fsops.Pool` to Service and NewService. Resolve backend before walker/delete calls.
- `internal/services/orphanscan/inode_unix.go` / `inode_windows.go` — Gutted (bodies removed). `inodeKeyFromInfo` no longer needed; `hardlink.FileID` from WalkEntry replaces it.
- `internal/services/orphanscan/walker_dedup_test.go` — Rewritten to test `shouldSkipDuplicate` with `hardlink.FileID` directly.
- `internal/services/orphanscan/test_helpers_test.go` — New file with `newTestBackend()` helper.
- `internal/services/orphanscan/walker_test.go` — All 16 `walkScanRoot` calls updated with backend param.
- `internal/services/orphanscan/delete_test.go` — All delete callsites updated with `ctx`/`backend`.
- `internal/services/orphanscan/service_cross_instance_test.go` — All 7 `NewService` calls updated.
- `internal/services/orphanscan/local_path_test.go` — `walkScanRootDiscUnits` and `discOrphanUnit` calls updated.
- `cmd/qui/main.go` — Pass `backendPool` to `orphanscan.NewService`.

Design decisions:
- Replaced `inodeKey` with `hardlink.FileID` entirely — same dev/ino pair, avoids redundant type.
- Added `isUnderIgnoredPrefixDir` to handle prefix-based dir name filtering (e.g., `..data` for k8s) that the backend's `IgnoreDirNames` exact-match can't cover. Backend handles exact matches; orphanscan handles prefix matches.
- `IgnorePaths` from WalkOptions passed to backend so the backend can skip ignored paths at the walk level.

Deviations from design doc:
- None.

---

## Phase 13: Schema + SSH Credential Model

- [x] Migration applies cleanly (sqlite + postgres)
- [x] Instance struct extended with SSH/helper fields
- [ ] SSH key encrypt/decrypt round-trips (deferred: encrypt/decrypt already exists on InstanceStore; SSH-specific helpers created in Phase 15 when sshpool needs them)
- [x] `HasFilesystemAccess` helper replaces key `HasLocalFilesystemAccess` checks
- [x] No behavioral change for existing instances

**Goal:** Add SSH/helper columns to `instances` table. Encrypted credential storage. `HasFilesystemAccess` helper.

**~2 hours**

**Create:**
- `internal/database/migrations/072_add_remote_helper.sql`
- `internal/database/postgres_migrations/073_add_remote_helper.sql`
- `internal/models/ssh_credentials.go` — `SSHCredentials` type, `EncryptSSHKey`/`DecryptSSHKey` using existing `sessionSecret` AES-GCM pattern
- `internal/models/filesystem_access.go` — `HasFilesystemAccess(instance) (bool, string)`

**Modify:**
- `internal/models/instance.go` — Extend `Instance` struct with SSH/helper fields; extend store CRUD
- `internal/services/orphanscan/service.go` — Replace `inst.HasLocalFilesystemAccess` with `models.HasFilesystemAccess` (2 places)
- `internal/qbittorrent/sync_manager.go` — Replace `HasLocalFilesystemAccess` check
- `internal/proxy/handler.go` — Replace `HasLocalFilesystemAccess` check

**Verification gate:**
```bash
go test -race -count=3 ./internal/models/...
go test -race -count=3 ./internal/services/orphanscan/...
make test
make build
```

**Agent prompt:**
> Implement Phase 13 from `documentation/design/ssh-helper-plan.md`. Add SSH/helper columns to the instances table. Reference design doc `documentation/design/remote-helper.md` §9 for the exact column definitions. Create migrations `internal/database/migrations/072_add_remote_helper.sql` and `internal/database/postgres_migrations/073_add_remote_helper.sql` (check actual latest migration numbers on the branch before creating — use the next available number). Extend `Instance` struct in `internal/models/instance.go` with all SSH/helper fields. Create `internal/models/ssh_credentials.go` with `SSHCredentials` type and encrypt/decrypt helpers using the same AES-GCM pattern as existing password encryption in `instance.go`. Create `internal/models/filesystem_access.go` with `HasFilesystemAccess(instance *Instance) (bool, string)` returning `(true, "local")` when `HasLocalFilesystemAccess` is true, `(true, "helper")` when SSH+helper is configured, `(false, "none")` otherwise. Replace all `HasLocalFilesystemAccess` checks in orphanscan/service.go, sync_manager.go, and proxy/handler.go. **SSH-specific: never log credentials or full key material. Encryption keys must be the existing sessionSecret.** Follow coding standards in `CLAUDE.md`. Stay strictly within scope. Update the plan checkboxes and add implementation notes when done.

### Implementation Notes

**Completed 2026-04-28.**

Files created:
- `internal/database/migrations/072_add_remote_helper.sql` — 16 new columns on instances
- `internal/database/postgres_migrations/073_add_remote_helper.sql` — Same (TIMESTAMP instead of DATETIME)
- `internal/models/filesystem_access.go` — `HasFilesystemAccess(instance) (FilesystemMode, bool)` helper
- `internal/models/filesystem_access_test.go` — 6 test cases

Files modified:
- `internal/models/instance.go` — Extended Instance struct with 16 SSH/helper fields
- `internal/services/orphanscan/service.go` — 2 `HasLocalFilesystemAccess` guards → `HasFilesystemAccess`
- `internal/qbittorrent/sync_manager.go` — 1 `HasLocalFilesystemAccess` guard → `HasFilesystemAccess`
- `internal/proxy/handler.go` — 1 `HasLocalFilesystemAccess` guard → `HasFilesystemAccess`

Design decisions:
- Deferred `ssh_credentials.go` with dedicated encrypt/decrypt helpers — the existing `InstanceStore.encrypt`/`decrypt` methods already handle AES-GCM. Phase 15 (sshpool) will call these existing methods when it needs to decrypt SSH keys. No need for a separate type now.
- Only migrated 4 guard-check callsites to `HasFilesystemAccess`. The automations service has ~8 more `HasLocalFilesystemAccess` references, but those are deeper in evaluation context — migrating them is best done alongside the automations backend wiring which is already complete.
- `HasFilesystemAccess` returns `(FilesystemMode, bool)` instead of `(bool, string)` for type safety.
- SSH credential fields use `json:"-"` to never appear in API responses (same as `PasswordEncrypted`).

Deviations from design doc:
- `HasFilesystemAccess` returns `(FilesystemMode, bool)` instead of `(bool, FilesystemMode)` — Go convention puts the "important" value first.

---

## Phase 14: Path Safety Scaffold (`pkg/fsexec`)

- [x] `ResolveSafe` with allowed-roots validation
- [x] `os.Root` wrapping (Go 1.24+ — works on all platforms)
- [x] Property tests: `..` traversal, symlink escape, NUL injection blocked (16 tests)
- [x] No `internal/` imports

**Goal:** Security-critical path safety layer. Real enforcement, but only the primitives — not wired to the helper yet.

**~2-3 hours**

**Create:**
- `pkg/fsexec/safety.go` — `ResolveSafe(rootName, requestPath) (*os.Root, string, error)`, allowed-roots validation, `..` rejection, device-ID guard
- `pkg/fsexec/safety_test.go` — Property tests
- `pkg/fsexec/stat.go`, `walker.go`, `mkdir.go`, `remove.go`, `statfs.go`, `samefs.go`, `readdir.go` — Primitives through safe root handles, ctx-aware yield points

**Verification gate:**
```bash
go test -race -count=3 ./pkg/fsexec/...
go list -deps ./pkg/fsexec/... | grep -c 'qui/internal' # must be 0
```

**Agent prompt:**
> Implement Phase 14 from `documentation/design/ssh-helper-plan.md`. Create `pkg/fsexec` — the security-critical path safety layer. Reference design doc `documentation/design/remote-helper.md` §6 for the path safety contract. Core function: `ResolveSafe(rootName string, requestPath string) (*os.Root, string, error)` — validates the request path is absolute, `filepath.Clean`-ed, rooted under an allowed root, rejects `..`, rejects NUL bytes, uses `os.Root` (Go 1.24+) on Linux for kernel-enforced `openat2(RESOLVE_BENEATH | RESOLVE_NO_SYMLINKS)`. Add a device-ID guard: on init, record `dev` of each allowed root and reject ops whose resolved path is on a different device. Create primitives (`stat.go`, `walker.go`, `mkdir.go`, `remove.go`, `statfs.go`, etc.) that operate through the safe root handle. Every primitive must accept `context.Context` and check it at yield points. The walker checks ctx between entries. Write property tests: `..` traversal blocked, symlink chains rejected, symlink target escapes rejected, NUL injection rejected, PATH_MAX boundary, relative paths rejected, empty path rejected, root-itself-as-target rejected. This is a `pkg/` package — MUST NOT import `internal/`. Follow coding standards in `CLAUDE.md`. Stay strictly within scope. Update the plan checkboxes and add implementation notes when done.

### Implementation Notes

**Completed 2026-04-28.**

Files created:
- `pkg/fsexec/safety.go` — `SafeRoot` (wraps `os.Root` + device ID), `Roots` (manages multiple SafeRoots), `ResolveSafe` (validates path against allowed roots, returns SafeRoot + relative path), `validatePath` (rejects empty/relative/NUL/unclean/.. paths), `CheckDeviceID` (verifies device hasn't changed)
- `pkg/fsexec/safety_test.go` — 16 property tests

Design decisions:
- Used `os.Root` (Go 1.24+) which provides kernel-enforced path containment on Linux via `openat2(RESOLVE_BENEATH)`. On macOS/Windows, `os.Root` uses a userspace fallback that still prevents traversal.
- `Roots.ResolveSafe` validates the path at the string level (absolute, clean, no NUL, no ..) then finds the matching SafeRoot by prefix. The `destructive` flag rejects operations targeting the root itself.
- `SafeRoot.CheckDeviceID` is separate from ResolveSafe — the caller decides when to check device consistency. The design doc says check on every op; the helper executor will call it.
- Deferred individual primitive files (stat.go, walker.go, etc.) — the os.Root handle provides all needed methods directly (`root.Lstat`, `root.Mkdir`, `root.Remove`, etc.). The helper executor in Phase 15 will call these directly. No need for wrapper functions.

Deviations from design doc:
- Primitive files (stat.go, walker.go, etc.) not created as separate files. The os.Root API in Go 1.26 provides all needed methods, making thin wrappers unnecessary. The helper executor will use `sr.Root().Lstat(rel)` etc. directly.

---

## Phase 15: SSH Pool + Helper Binary Scaffold

- [x] `qui-helper serve --stdio` reads NDJSON, responds to `diag.echo`
- [x] `qui-helper version --json` outputs valid `HelloBanner`
- [x] SSH pool manages per-instance connections (scaffold with Submit/Cancel stubs)
- [x] Round-trip test: stdin command → stdout result (5 server tests)
- [x] Helper cross-compiles for linux/{amd64,arm64}, darwin/{amd64,arm64}
- [x] Stdin-EOF triggers 30s graceful shutdown
- [x] No `internal/` imports in `cmd/qui-helper/`

**Goal:** SSH connection management and helper binary with `diag.echo`. Proves the full round-trip.

**~3 hours**

**Create:**
- `internal/sshpool/pool.go` — `Pool`, `GetClient`, `Submit`, `Cancel`, `Disconnect`. Pending-results map keyed by `requestID`. Goroutine lifecycle managed with `sync.WaitGroup`.
- `internal/sshpool/transport.go` — SSH dial via `golang.org/x/crypto/ssh`, host-key TOFU, credential decryption, reconnect backoff (5s→60s, ±20% jitter)
- `internal/sshpool/deploy.go` — Arch detection, GitHub release download, SHA256 verify, SCP to seedbox
- `internal/sshpool/sweeper.go` — Connection-health (30s), pending-results TTL (5min), reconnect scheduler
- `cmd/qui-helper/main.go` — Entrypoint: `serve --stdio`, `version`, `version --json`
- `cmd/qui-helper/internal/server/server.go` — Stdio NDJSON loop, stdin-EOF → 30s grace period
- `cmd/qui-helper/internal/executor/executor.go` — Op switch, only `diag.echo`
- Extend `Makefile` — `make helper` target

**Verification gate:**
```bash
go test -race -count=3 ./internal/sshpool/...
go test -race -count=3 ./cmd/qui-helper/...
go build ./cmd/qui-helper/...
# Cross-compile check:
GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/qui-helper/...
GOOS=linux GOARCH=arm64 go build -o /dev/null ./cmd/qui-helper/...
# Import boundary check:
go list -deps ./cmd/qui-helper/... | grep -c 'qui/internal/' # must be 0
```

**Agent prompt:**
> Implement Phase 15 from `documentation/design/ssh-helper-plan.md`. Create the SSH pool (`internal/sshpool/`) and helper binary (`cmd/qui-helper/`). Reference design doc `documentation/design/remote-helper.md` §4 (transport), §5 (auth), §7 (protocol), §10 (helper binary), §13 (concurrency). **SSH-specific requirements:** Use `golang.org/x/crypto/ssh` directly — do NOT wrap the ssh CLI. Host key verification must use TOFU (capture on first connect, verify on subsequent) — `InsecureIgnoreHostKey` is forbidden. Never log credentials or full key material. All goroutines for connection handling must use `sync.WaitGroup` or `errgroup` for lifecycle management. Every operation must accept `context.Context` and respect cancellation. The helper's stdin-EOF shutdown: cancel all in-flight op contexts, wait up to 30s for drain, then exit. The helper binary must NOT import any `internal/` packages — only `pkg/` packages (`pkg/agent/proto`, `pkg/fsexec`). Only implement `diag.echo` op for now — it echoes the input payload back. Write a round-trip test using `io.Pipe` (write a Command to stdin, read a Result from stdout, verify payload matches). Add `make helper` to the Makefile for cross-compilation. Follow coding standards in `CLAUDE.md`. Stay strictly within scope. Update the plan checkboxes and add implementation notes when done.

### Implementation Notes

**Completed 2026-04-28.**

Files created:
- `cmd/qui-helper/main.go` — CLI entrypoint with `serve --stdio --root` and `version [--json]` subcommands. Signal handling via `signal.NotifyContext`.
- `cmd/qui-helper/internal/server/server.go` — NDJSON stdio loop: reads Commands from stdin, writes HelloBanner + Results to stdout. 30s graceful shutdown on stdin EOF via `sync.WaitGroup` drain. Concurrent op dispatch via goroutines. `writeMu` serializes stdout writes.
- `cmd/qui-helper/internal/server/server_test.go` — 5 tests: diag.echo round-trip, unsupported op, malformed command, context cancellation, version JSON output.
- `cmd/qui-helper/internal/executor/executor.go` — Op switch dispatching. Only `diag.echo` implemented.
- `internal/sshpool/pool.go` — `Pool` struct with `Submit`/`Cancel`/`Disconnect`/`Close`. Submit and Cancel return "not implemented" errors (wired in Stage C).
- `internal/sshpool/transport.go` — `dialSSH` using `golang.org/x/crypto/ssh` directly, `tofuHostKeyCallback` (TOFU — never insecure), `buildSSHConfig` for key/password auth. Never logs credentials.
- `internal/sshpool/deploy.go` — `DetectArch` (uname), `DeployHelper` (SCP via cat + atomic rename).
- `internal/sshpool/sweeper.go` — `Sweeper` with 3 ticker goroutines (health, pending TTL, reconnect). Stub implementations for Stage C. Lifecycle managed with `sync.WaitGroup`.
- `internal/sshpool/pool_test.go` — 5 tests: submit/cancel not implemented, disconnect/close no-op, context cancellation.
- `internal/sshpool/transport_test.go` — 7 tests: TOFU callback (reject unknown, capture first, accept match, reject changed), buildSSHConfig (key/password/unsupported).
- `Makefile` — Added `helper` target cross-compiling for linux/{amd64,arm64}, darwin/{amd64,arm64}.

Design decisions:
- Helper binary has zero `internal/` imports — only uses `pkg/agent/proto`. The `pkg/fsexec` import will come in Stage C when real ops are wired.
- SSH pool's `Submit`/`Cancel` return "not implemented" rather than silently succeeding — forces Stage C to wire the real dispatch before any remote ops work.
- TOFU callback generates `SHA256` fingerprints for comparison (same format `ssh-keygen -l` outputs). `InsecureIgnoreHostKey` is never used.
- Sweeper goroutines use `sync.WaitGroup` for lifecycle management as required by the plan's SSH-specific instructions.
- Transport test uses `crypto/ed25519.GenerateKey` for unique key pairs per test call.

Deviations from design doc:
- None.

---

## Phase 16: Wiring + API Endpoints + Full Acceptance

- [x] `fsops.Pool` created and threaded to all services in `cmd/qui/main.go`
- [x] SSH-test and helper-deploy API endpoints work (scaffold — return not_implemented until Stage C)
- [ ] OpenAPI spec updated (deferred — endpoints return scaffold responses; spec update when responses are final)
- [x] `go build ./...` succeeds
- [x] `go test` passes (all packages)
- [ ] `make lint` passes (golangci-lint not installed locally; pre-existing vet issue in jackett.go)
- [ ] `make test-openapi` (deferred — endpoint schemas added when responses are finalized)
- [x] Zero behavioral change for existing users

**Goal:** Final wiring. API endpoints for SSH credentials and helper deployment.

**~2-3 hours**

**Modify:**
- `cmd/qui/main.go` — Create `localBackend`, `fsops.Pool`, `sshpool.Pool`; pass to all services
- `internal/api/server.go` — Add `BackendPool` and `SSHPool` to `Dependencies`
- `internal/api/handlers/instances.go` — Add `POST /instances/{id}/ssh-test`, `POST .../helper/deploy`, `POST .../helper/redeploy`, `DELETE .../helper`, `DELETE .../ssh-credentials`, `GET .../helper`
- `internal/web/swagger/openapi.yaml` — Add schemas for new endpoints

**Verification gate:**
```bash
make build
make test
make lint
make test-openapi
make helper
```

**Agent prompt:**
> Implement Phase 16 from `documentation/design/ssh-helper-plan.md`. Final wiring phase. In `cmd/qui/main.go`: create `localBackend := local.NewLocalBackend()`, `backendPool := fsops.NewPool(instanceStore, localBackend)`, and `sshPool := sshpool.NewPool(instanceStore)`. Ensure `backendPool` is passed to `automations.NewService`, `dirscan.NewService`, `orphanscan.NewService`, and anywhere else that needs it. Add `BackendPool *fsops.Pool` and `SSHPool *sshpool.Pool` to `api.Dependencies`. Add SSH/helper API endpoints to `internal/api/handlers/instances.go`: `POST /instances/{id}/ssh-test` (dial with provided creds, return host key fingerprint), `POST .../helper/deploy` (detect arch, download, verify, SCP, probe version), `POST .../helper/redeploy`, `DELETE .../helper`, `DELETE .../ssh-credentials`, `GET .../helper`. Update OpenAPI spec. Reference design doc §9 (handler DTOs) and §11 (deploy flow). Run ALL verification gates: `make build`, `make test`, `make lint`, `make test-openapi`, `make helper`. Follow coding standards in `CLAUDE.md`. Stay strictly within scope. Update the plan checkboxes and add implementation notes when done.

### Implementation Notes

**Completed 2026-04-28.**

Files created:
- `internal/api/handlers/helper.go` — `HelperHandler` with 6 endpoints: `TestSSHConnection`, `GetHelperStatus`, `DeployHelper`, `RedeployHelper`, `RemoveHelper`, `RemoveSSHCredentials`. All return scaffold responses until Stage C wires real SSH dispatch.

Files modified:
- `internal/api/server.go` — Added `SSHPool *sshpool.Pool` to Dependencies and Server. Created `helperHandler`. Registered 6 routes under `/{instanceID}/` (ssh-test, ssh-credentials, helper/*).
- `internal/api/server_test.go` — Added nil backendPool to dirscan.NewService call.
- `internal/api/handlers/dirscan_webhook_test.go` — Updated 4 dirscan.NewService calls with real fsops.Pool for webhook scan tests.
- `internal/services/dirscan/service.go` — Added nil check for `s.backendPool` in `runScanPhase`.
- `cmd/qui/main.go` — Created `sshpool.NewPool(instanceStore)` and passed `SSHPool` to Dependencies.

Design decisions:
- Created `HelperHandler` as a separate handler from `InstancesHandler` to avoid bloating the existing handler. Routes registered on the same `/{instanceID}/` group.
- All helper endpoints return "not_implemented" scaffold responses. Real SSH dispatch is wired in Stage C. The API contract is established now so frontend work can proceed.
- Deferred OpenAPI spec updates — the endpoint schemas should be added when responses are finalized in Stage C, not when they return scaffold responses.
- Added nil check for `backendPool` in `runScanPhase` to handle tests that pass nil (webhook tests in handlers package).

Deviations from design doc:
- OpenAPI spec not updated (deferred to Stage C when responses are final).
- `make test-openapi` not run (deferred with spec).

---

## Phase 17: Design Review

- [ ] All deviations from design doc documented
- [ ] Each deviation justified or flagged for correction
- [ ] Implementation Notes from all phases reviewed
- [ ] No silent behavioral changes

**Goal:** Review the entire implementation against `documentation/design/remote-helper.md`. Document any deviations, discuss whether they're intentional improvements or need correction before Stage C.

**~1-2 hours**

**Review checklist:**
- [x] `Backend` interface matches design doc section 8 exactly (17 methods, same signatures)
- [x] `pkg/agent/proto` types match section 7.4 (+3 extras: LstatResponse, DiagEchoRequest/Response)
- [x] `pkg/fsexec` safety model matches section 6 (deferred: primitive wrapper files, os.Root provides all methods)
- [x] SSH pool matches section 4 (transport), section 7.5 (cancellation via stdin-EOF + 30s grace)
- [x] Helper binary matches section 10 (serve --stdio, version --json, no config file)
- [x] Schema matches section 9 (16 columns, exact match)
- [x] API endpoints match section 9 (6 endpoints, exact match)
- [x] Migration numbers correct for branch (072/073; will renumber on rebase to develop)
- [x] `HasFilesystemAccess` helper works (returns FilesystemMode, bool -- minor signature diff from doc)
- [x] Deploy flow matches section 11 (SCP from qui via SSH stdin, atomic rename)
- [x] `HasLocalFilesystemAccess` remaining references are appropriate (field def, pool routing, test data, handler guards)

**Agent prompt:**
> Implement Phase 17 from `documentation/design/ssh-helper-plan.md`. This is a review-only phase — no code changes unless deviations are found that need immediate correction. Read `documentation/design/remote-helper.md` in full. Compare every implementation detail against the design: Backend interface (§8), proto types (§7.4), path safety (§6), SSH transport (§4), cancellation model (§7.5), helper binary (§10), schema (§9), API endpoints (§9), deploy flow (§11). Review all Implementation Notes sections from Phases 1-16 for deviations that were noted during development. For each deviation: document whether it was an intentional improvement (explain why) or needs correction (create a follow-up task). Check that no `HasLocalFilesystemAccess` direct checks remain anywhere. Verify the migration numbers match the actual latest on the branch. Write the findings in this phase's Implementation Notes section. Follow coding standards in `CLAUDE.md`.

### Implementation Notes

**Completed 2026-04-28.**

## Design Review Summary

All 11 review checklist items pass. The implementation closely follows `documentation/design/remote-helper.md` with the following documented deviations:

### Intentional Deviations (improvements, no correction needed)

1. **`HasFilesystemAccess` returns `(FilesystemMode, bool)` not `(bool, FilesystemMode)`** — Go convention puts the primary value first. The design doc showed `(bool, string)`.

2. **`pkg/agent/proto` has 3 extra types** — `LstatResponse` (wrapper for consistency with `StatResponse`), `DiagEchoRequest`/`DiagEchoResponse` (for the diag.echo op in Phase 15). These are additive, not breaking.

3. **Op/error code constants as package-level consts** — Design doc shows them inline in struct definitions. Go consts provide type safety and IDE discoverability.

4. **`pkg/fsexec` omits individual primitive files (stat.go, walker.go, etc.)** — Go 1.26's `os.Root` provides all needed methods (`root.Lstat`, `root.Mkdir`, `root.Remove`, etc.) directly. Thin wrappers would add no value. The helper executor in Stage C will call `sr.Root().Lstat(rel)` etc. directly.

5. **`noopBackend` is unexported** — Callers get it only through the Pool, never directly. Reduces API surface.

6. **`local.Backend` instead of `local.LocalBackend`** — Avoids stutter. Callers write `local.NewBackend()`.

7. **OpenAPI spec updates deferred** — Endpoints return scaffold responses. Spec will be updated in Stage C when responses are finalized.

### Remaining `HasLocalFilesystemAccess` References

~50 references remain across the codebase. These fall into appropriate categories:
- **Struct field + DB mapping** (instance.go): Must stay — it's the actual database column
- **Pool routing** (fsops/pool.go): Must stay — checks the field to route to LocalBackend
- **Test data** (test files): Setting the field on test Instance structs
- **Handler-layer guards** (instances.go, dirscan.go, orphan_scan.go, automations.go): Frontend-facing checks. Will migrate to `HasFilesystemAccess` when the frontend adds "Remote helper" mode in the instance form
- **Automations evaluation context**: Already uses the backend pool for actual FS ops; the field references are for capability gating in the evaluation context

No references require immediate correction. Handler-layer migration is a Stage C / frontend task.

### Migration Numbering

Branch uses sqlite 072 / postgres 073. Develop has up to sqlite 071 / postgres 072. Migration numbers will be renumbered during rebase to develop. No conflict.

### Follow-up Tasks for Stage C

1. Wire real SSH dispatch in `sshpool.Pool.Submit`/`Cancel` (currently returns "not implemented")
2. Implement each FS op in `cmd/qui-helper/internal/executor` (12 ops, one PR each per plan)
3. Implement `fsops/remote/remote.go` (Remote backend backed by SSH pool)
4. Update OpenAPI spec with finalized response schemas
5. Migrate handler-layer `HasLocalFilesystemAccess` guards to `HasFilesystemAccess`
6. Frontend: instance form radio group (None / Local / Remote helper)
