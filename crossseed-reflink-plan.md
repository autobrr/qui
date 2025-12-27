# Cross-seed Reflink Support Plan (Coder-Ready)

## Goal
Add **reflink (copy-on-write) support** for cross-seeding so qui can safely allow qBittorrent to download/repair bytes that would otherwise risk corrupting the original seeded files (hardlinks/reuse).

This plan is written to avoid ambiguity: follow it as a checklist.

## Supported platforms (compile vs runtime)
qui must **compile** for the OSes we ship:
- `linux`, `darwin`, `windows`, `freebsd` (see `.goreleaser.yml` and `ci.Dockerfile`)

Reflink functionality is only **supported at runtime** when the OS + filesystem + mount setup support copy-on-write cloning.
This must be detected by an actual probe (`SupportsReflink`) at runtime (do **not** hardcode filesystem names).
Practical notes (for user expectations):
- **Linux:** reflink is commonly available on `btrfs` and `xfs`; it is commonly **unavailable** on `overlayfs` and `ext4` (treat these as “likely unsupported”, but rely on the probe).
- **macOS:** available on APFS via `clonefile` (again, rely on the probe).

On **Windows** and **FreeBSD**: feature must still compile, but always report “unsupported” at runtime (no fallback copy implementation in this phase).

## UX: make mode selection explicit (no “auto” / no fallback)
Users must explicitly choose per instance:
- **Hardlink mode** (existing behavior)
- **Reflink mode** (new behavior)

Implementation detail:
- Keep existing per-instance hardlink fields.
- Add a new per-instance reflink enable field.
- Enforce mutual exclusivity:
  - UI must prevent enabling both.
  - Backend must validate incoming updates so both cannot be true at once (reject with a clear error).

## Behavioral rules (must match exactly)

### Hardlink mode (existing + strict safety)
- If `internal/services/crossseed/piece_boundary.go` detects unsafe piece boundaries for missing/extra files:
  - **Do not add** the torrent in hardlink mode.
  - Return the existing skip status (`skipped_unsafe_pieces`).
- Hardlink mode continues to support “safe extras”:
  - If extra/missing files exist but the piece-boundary check says it is safe:
    - Create hardlink tree for content files.
    - Add torrent paused.
    - Trigger recheck and use the existing recheck worker for auto-resume threshold.

### Reflink mode (new + bypass piece-boundary skipping)
- Reflink mode must **never** skip due to piece-boundary safety.
  - The whole point is that reflinks allow qBittorrent to write/repair the copy-on-write clone without touching originals.
  - Therefore, when reflink mode is selected, do not return `skipped_unsafe_pieces` even if unsafe boundaries exist.
- Reflink mode must use the **same “recheck worker” logic** as reuse/hardlink:
  - Add torrent paused.
  - Trigger recheck explicitly.
  - If `SkipAutoResume` is enabled → leave paused.
  - Otherwise queue the existing recheck-resume worker (`queueRecheckResume`).
  - If recheck completion is below threshold → **leave paused for manual review** (do not delete automatically).
- If `req.SkipRecheck == true` and the add requires recheck:
  - Do not add; return `skipped_recheck`.
  - In reflink mode we always treat the add as “requires recheck” (see next section).

### Recheck policy for reflink mode (explicit)
Reflink mode must **always** trigger a recheck after adding, because reflink mode is explicitly allowed to tolerate/repair mismatches and qBittorrent must verify piece hashes before safely seeding/resuming.

Consequences:
- Reflink adds are always “requires recheck”.
- If a user has `SkipRecheck` enabled, reflink mode must return `skipped_recheck` and do nothing else.

## Data model + migrations

### Database
Add new per-instance column:
- `use_reflinks BOOLEAN NOT NULL DEFAULT 0`

Migration requirements:
- Add a new migration file in `internal/database/migrations/` similar to `040_move_hardlink_to_instance.sql`.
- Update `instances_view` to include `i.use_reflinks`.
- Update `internal/database/db_test.go` schema expectations to include the new column.

### Backend models + API
Update `internal/models/instance.go`:
- Add `UseReflinks bool \`json:"useReflinks"\`` to `Instance`
- Include it in `MarshalJSON` and `UnmarshalJSON`
- Ensure store scan/update paths include the new DB column (follow existing patterns for `UseHardlinks`).

Update `internal/api/handlers/instances.go`:
- Add to `UpdateInstanceRequest`: `UseReflinks *bool \`json:"useReflinks,omitempty"\``
- Add to `InstanceResponse`: `UseReflinks bool \`json:"useReflinks"\``
- Add validation: reject updates that would result in both `UseHardlinks==true` and `UseReflinks==true`.

### Frontend types + UI
Update `web/src/types/index.ts` instance types:
- Add `useReflinks: boolean` (and optional in patch type)

Update `web/src/pages/CrossSeedPage.tsx`:
- Add “Reflink mode (copy-on-write)” per instance.
- Ensure toggles are mutually exclusive in UI:
  - If enabling reflink, disable hardlink (and vice versa) before sending the update.
- Validation:
  - Requires `hasLocalFilesystemAccess=true`
  - Requires `hardlinkBaseDir` to be set (reusing same base dir/preset)
- UI copy must explicitly state:
  - “Reflink mode always triggers recheck”
  - “If below tolerance threshold, torrent remains paused for manual review”

## Filesystem implementation: `pkg/reflinktree`
Create a new package `pkg/reflinktree` that mirrors the “tree creation” model used by hardlinks.

### API surface (exact)
- `func Create(plan *hardlinktree.TreePlan) error`
- `func Rollback(plan *hardlinktree.TreePlan) error`
- `func SupportsReflink(dir string) (supported bool, reason string)`

### OS-specific cloning (build tags)
- Linux:
  - Use `golang.org/x/sys/unix.IoctlFileClone(destFd, srcFd)` for whole-file clone.
- macOS:
  - Use `golang.org/x/sys/unix.Clonefile(src, dst, 0)`.
- Unsupported OS (windows/freebsd):
  - `SupportsReflink` returns false with stable reason.
  - `Create` returns an explicit error like `ErrReflinkUnsupported`.

### `SupportsReflink` behavior (required)
It must be a **real capability probe**, not a guess:
- Create two small temp files inside `dir` and attempt a clone from src → dst.
- If clone succeeds, return supported=true.
- If clone fails with “operation not supported”, return supported=false with a reason string.
- Always clean up temp files.

### Tree creation semantics
- `Create(plan)`:
  - Ensure parent dirs for targets exist (`os.MkdirAll`).
  - Clone each file from `FilePlan.SourcePath` → `FilePlan.TargetPath`.
  - On first failure: rollback any previously created targets and return the error.
- `Rollback(plan)`:
  - Best-effort remove created target files and then remove empty dirs under the plan root.
  - Never delete outside `plan.RootDir`.

## Cross-seed service integration (required structure)

### Add new mode handler: `processReflinkMode`
Add a new function in `internal/services/crossseed/service.go` similar in structure to `processHardlinkMode`, returning a `reflinkModeResult` with:
- `Used bool`
- `Success bool`
- `Result InstanceCrossSeedResult`

It must:
- Load the instance model
- Check `instance.UseReflinks`
- Validate base dir configured (`instance.HardlinkBaseDir`)
- Validate `instance.HasLocalFilesystemAccess`
- Validate same filesystem (use the same `fsutil.SameFilesystem` check as hardlink)
- Probe reflink support: `reflinktree.SupportsReflink(instance.HardlinkBaseDir)`
- If any eligibility check fails and reflink is enabled:
  - Return `Used=true` with `Status="reflink_error"` and an actionable message (no silent fallback)

### Where reflink is selected
In `processCrossSeedCandidate`, mode selection must occur before enforcing `skipped_unsafe_pieces`:
- Determine “selected mode” from instance settings:
  - If `UseReflinks` → select reflink mode
  - Else if `UseHardlinks` → select hardlink mode
  - Else → reuse mode
- If selected mode is reflink:
  - Do NOT return `skipped_unsafe_pieces`; proceed to `processReflinkMode`.
- If selected mode is hardlink:
  - Keep existing hardlink behavior (strict piece-boundary gate).
- If selected mode is reuse:
  - Keep existing reuse behavior.

### Reflink plan building (how to choose which files to clone)
Reflink mode creates a tree by cloning existing on-disk files. It must:
- Build `existingFiles []hardlinktree.ExistingFile` from `candidateFiles` (matched torrent) like hardlink mode does (using `props.SavePath + f.Name`).
- Decide which incoming (source) files are “present on disk” using the same multiset matching approach already in use:
  - Key: `(normalizeFileKey(path), size)`
  - For each `sourceFiles` entry, if it can be matched in the candidate multiset, include it in the clone plan; otherwise it will be downloaded by qBittorrent.
- Create `candidateTorrentFilesToClone []hardlinktree.TorrentFile` from that selection.
- Build plan using `hardlinktree.BuildPlan(candidateTorrentFilesToClone, existingFiles, LayoutOriginal, torrentName, destDir)`.
- Create reflink tree with `reflinktree.Create(plan)`.

### Add torrent options + recheck flow (must match)
Reflink mode add must use:
- `autoTMM=false`
- `savepath=plan.RootDir`
- `contentLayout=Original`
- Always add paused/stopped:
  - `paused=true`, `stopped=true`
- Set tags/category same as existing hardlink mode does.
- `skip_checking=true` is allowed, but we must still explicitly:
  - call `BulkAction(..., "recheck")` immediately after add
  - then either queue auto-resume (`queueRecheckResume`) or leave paused if `SkipAutoResume==true`

### SkipRecheck behavior
Because reflink mode always requires recheck:
- If `req.SkipRecheck == true` and mode is reflink:
  - Return `skipped_recheck` (do not create reflink tree; do not add torrent).

### Below-threshold behavior
If the recheck completes below the configured threshold:
- Leave paused for manual review (existing worker already behaves this way).
- Do not auto-delete the torrent or reflink directory.
- Log clearly so users know why it stayed paused.

## Logging + statuses
Add new result status for reflink mode:
- `added_reflink` for successful add (recheck queued or auto-resume skipped)
- `reflink_error` for enabled reflink mode that fails eligibility or operations

Result `Message` must include:
- whether recheck was triggered
- whether auto-resume was queued or skipped
- reminder that low completion remains paused for manual review

## Docs update
Update `docs/CROSS_SEEDING.md`:
- Add a new “Reflink mode” section:
  - What it does (copy-on-write clones)
  - Requirements (local filesystem access, same filesystem, FS support)
  - Recheck is always triggered
  - Disk usage: starts near-zero; grows as blocks are modified; can approach full size in worst case
  - Below threshold remains paused for manual review

Update UI copy in `web/src/pages/CrossSeedPage.tsx` to match.

## Tests

### Unit tests for reflinktree
- Add tests that:
  - On unsupported OS build tags: `SupportsReflink` returns supported=false (no failure).
  - On linux/darwin: attempt probe in temp dir; if unsupported filesystem, `t.Skip` (must not fail CI).

### Cross-seed service tests
Add at least one test that verifies:
- With mode=hardlink and unsafe piece boundaries → status `skipped_unsafe_pieces`
- With mode=reflink and unsafe piece boundaries → we attempt reflink add path (no `skipped_unsafe_pieces`)

## Acceptance criteria (must pass)
- `go test ./...` passes on dev machine.
- Builds compile for `linux/windows/darwin/freebsd` (no runtime assumptions).
- UI shows per-instance reflink toggle, enforces exclusivity with hardlink, and shows correct warnings.
- Hardlink mode continues to skip unsafe extras; reflink mode never skips for piece-boundary.
- Reflink mode adds paused, triggers recheck, and uses the existing resume queue logic; below threshold stays paused.
