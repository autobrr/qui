# SSH Helper Build Plan

> **Location:** This plan will be committed to `documentation/design/ssh-helper-plan.md`
>
> **Design reference:** `documentation/design/remote-helper.md`
> **Coding standards:** `CLAUDE.md`

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

**General coding standards (from CLAUDE.md):**
- `gofmt`-clean, PascalCase exports, camelCase locals
- Interfaces max 5 methods (the 17-method `Backend` is the documented exception)
- Explicit error handling, no silent failures
- `go test -race -count=3` for all test runs
- golangci-lint v2 with project profile
- Conventional commits: `feat(scope):`, `fix(scope):`, etc.
- Never add Co-Authored-By or AI attribution to commits

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

- [ ] `os.Lstat` + `hardlink.GetFileID` → `backend.Lstat`
- [ ] Hardlink index scope maps unchanged
- [ ] Tests pass

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
_(filled in after phase completion)_

---

## Phase 8: Refactor — `qbittorrent/delete_cleanup.go`

- [ ] `os.Stat` → `backend.Stat`
- [ ] `os.Remove` → `backend.Remove`
- [ ] Backend flows through delete action call chain
- [ ] Tests pass

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
_(filled in after phase completion)_

---

## Phase 9: Refactor — `dirscan/fileid_index.go`

- [ ] `os.Stat` + `hardlink.GetFileID` → `backend.Lstat`
- [ ] Dirscan service gains `backendPool`
- [ ] Tests pass

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
_(filled in after phase completion)_

---

## Phase 10: Refactor — `dirscan/inject.go` + `crossseed/FindMatchingBaseDir`

- [ ] `FindMatchingBaseDir` uses `backend.MkdirAll` + `backend.SameFilesystem`
- [ ] All 3 callsites updated (crossseed:11125, crossseed:11715, inject:587)
- [ ] `createLinkTree` uses `backend.HardlinkTree`/`ReflinkTree`/`SupportsReflink`
- [ ] `rollbackLinkTree` uses `backend.RemoveTree`/`Remove`
- [ ] Tests pass for both dirscan and crossseed

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
_(filled in after phase completion)_

---

## Phase 11: Refactor — `dirscan/scanner.go`

- [ ] `filepath.WalkDir` callback → `backend.WalkDir` channel consumption
- [ ] `os.ReadDir` → `backend.ReadDir`
- [ ] `os.Stat` → `backend.Stat`
- [ ] Same searchees found, same files, same sizes
- [ ] Context cancellation mid-scan works

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
_(filled in after phase completion)_

---

## Phase 12: Refactor — `orphanscan/walker.go` + `delete.go`

- [ ] Walker: `filepath.WalkDir` → `backend.WalkDir` channel
- [ ] Walker: `os.ReadDir` → `backend.ReadDir`
- [ ] Delete: `os.Lstat`/`os.Remove`/`os.RemoveAll` → backend equivalents
- [ ] Orphanscan service gains `backendPool`
- [ ] All orphanscan tests pass (including cross-instance)
- [ ] Delete safety checks preserved

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
_(filled in after phase completion)_

---

## Phase 13: Schema + SSH Credential Model

- [ ] Migration applies cleanly (sqlite + postgres)
- [ ] Instance struct extended with SSH/helper fields
- [ ] SSH key encrypt/decrypt round-trips
- [ ] `HasFilesystemAccess` helper replaces `HasLocalFilesystemAccess` checks
- [ ] No behavioral change for existing instances

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
_(filled in after phase completion)_

---

## Phase 14: Path Safety Scaffold (`pkg/fsexec`)

- [ ] `ResolveSafe` with allowed-roots validation
- [ ] `os.Root` wrapping on Linux (Go 1.24+)
- [ ] Property tests: `..` traversal, symlink escape, NUL injection blocked
- [ ] No `internal/` imports

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
_(filled in after phase completion)_

---

## Phase 15: SSH Pool + Helper Binary Scaffold

- [ ] `qui-helper serve --stdio` reads NDJSON, responds to `diag.echo`
- [ ] `qui-helper version --json` outputs valid `HelloBanner`
- [ ] SSH pool manages per-instance connections
- [ ] Round-trip test: stdin command → stdout result
- [ ] Helper cross-compiles for linux/{amd64,arm64}, darwin/{amd64,arm64}
- [ ] Stdin-EOF triggers 30s graceful shutdown
- [ ] No `internal/` imports in `cmd/qui-helper/`

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
_(filled in after phase completion)_

---

## Phase 16: Wiring + API Endpoints + Full Acceptance

- [ ] `fsops.Pool` created and threaded to all services in `cmd/qui/main.go`
- [ ] SSH-test and helper-deploy API endpoints work
- [ ] OpenAPI spec updated
- [ ] `make build` succeeds
- [ ] `make test` passes (full suite)
- [ ] `make lint` passes
- [ ] `make test-openapi` passes
- [ ] Zero behavioral change for existing users

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
_(filled in after phase completion)_

---

## Phase 17: Design Review

- [ ] All deviations from design doc documented
- [ ] Each deviation justified or flagged for correction
- [ ] Implementation Notes from all phases reviewed
- [ ] No silent behavioral changes

**Goal:** Review the entire implementation against `documentation/design/remote-helper.md`. Document any deviations, discuss whether they're intentional improvements or need correction before Stage C.

**~1-2 hours**

**Review checklist:**
- [ ] `Backend` interface matches §8 exactly (17 methods, same signatures)
- [ ] `pkg/agent/proto` types match ��7.4
- [ ] `pkg/fsexec` safety model matches §6
- [ ] SSH pool matches §4 (transport), §7.5 (cancellation/crash recovery)
- [ ] Helper binary matches §10 (subcommands, logging, no config file)
- [ ] Schema matches §9 (all columns present)
- [ ] API endpoints match §9 (handler DTOs)
- [ ] Migration numbers are correct for current branch state
- [ ] `HasFilesystemAccess` helper matches §9 description
- [ ] Deploy flow matches §11 (SCP from qui, not curl from seedbox)
- [ ] No unintended `HasLocalFilesystemAccess` references remain

**Agent prompt:**
> Implement Phase 17 from `documentation/design/ssh-helper-plan.md`. This is a review-only phase — no code changes unless deviations are found that need immediate correction. Read `documentation/design/remote-helper.md` in full. Compare every implementation detail against the design: Backend interface (§8), proto types (§7.4), path safety (§6), SSH transport (§4), cancellation model (§7.5), helper binary (§10), schema (§9), API endpoints (§9), deploy flow (§11). Review all Implementation Notes sections from Phases 1-16 for deviations that were noted during development. For each deviation: document whether it was an intentional improvement (explain why) or needs correction (create a follow-up task). Check that no `HasLocalFilesystemAccess` direct checks remain anywhere. Verify the migration numbers match the actual latest on the branch. Write the findings in this phase's Implementation Notes section. Follow coding standards in `CLAUDE.md`.

### Implementation Notes
_(filled in after phase completion)_
