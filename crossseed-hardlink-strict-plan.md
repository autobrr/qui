# Plan

Make cross-seed safety consistent across **reuse mode** and **hardlink mode** by adding a **piece-boundary safety check** for cases where we allow missing “extra” files via `ignorePatterns` / size tolerance. Only allow cross-seeds that could require qBittorrent to download extra files when those extra files **cannot share pieces** with the already-present content files, preventing qBittorrent from overwriting bytes inside existing content files when completing the torrent.

This replaces the current “partial hardlink mode” behavior with a deterministic rule:
- Exact matches proceed normally.
- Partial matches are only allowed when the “missing” portion is provably confined to non-content files (via piece-boundary checks).
- Otherwise, the match is **skipped** (never added to qBittorrent).

## Scope
- In: Implement piece-boundary safety checks for “extras allowed by ignorePatterns” in both reuse and hardlink modes; mark unsafe cases as `skipped`; remove/disable the current “partial hardlink mode” path; add tests + docs/UI updates; keep `skipRecheck` semantics (users can opt out and only accept matches that never require a recheck).
- Out: Reflink/COW support; content hashing; forcing rechecks (beyond existing behavior); allowing partial matches that would require downloading missing pieces that overlap content files.

## Action items
[ ] Reproduce and document the failure mode using existing log evidence (e.g., `d22e...` shows manual `recheck` then `Partial hardlink aborted ... reason=required hardlinked files incomplete after recheck`), and map it to the current “partial hardlink mode” code path.
[ ] Define the new “extras safety” contract (backend-enforced, applies to both reuse and hardlink):
[ ] - Non-ignored (“content”) files must be present and must size-match exactly (existing `hasContentFileSizeMismatch` guard remains mandatory).
[ ] - Ignored files may be missing and may be downloaded by qBittorrent **only if** no torrent piece spans both content bytes and ignored/missing bytes.
[ ] - If this cannot be proven from `.torrent` metadata, treat the match as `skipped` (never add).
[ ] Implement a reusable piece-boundary safety helper (likely in `internal/services/crossseed/`), given:
[ ] - incoming torrent file list order + sizes,
[ ] - `piece length`,
[ ] - which incoming files are “content present” vs “ignored/missing allowed”,
[ ] and returning `{safe bool, reason string, boundaryOffsets []int64}`.
[ ] Integrate the piece-boundary safety decision into **reuse mode**:
[ ] - When `ignorePatterns` are being used to allow missing files (or any case that would rely on tolerance/“resume at <100%”), run the piece-boundary check.
[ ] - If unsafe: mark as `skipped` with a clear reason (e.g., “ignored files share pieces with content; would require reflink/copy mode”).
[ ] - If safe: allow the existing recheck/auto-resume workflow (and continue honoring the user’s `skipRecheck` setting: if recheck is required and `skipRecheck=true`, skip as today).
[ ] Integrate the same piece-boundary safety decision into **hardlink mode**:
[ ] - Remove/disable the current “partial hardlink mode” implementation (hardlink some + download some + rollback-on-recheck outcomes).
[ ] - Replace it with: hardlink all required content files; allow ignored missing files only when the piece-boundary check is safe; otherwise skip.
[ ] - Preserve user opt-out: if `skipRecheck=true` and the scenario requires recheck (alignment or missing ignored files), skip (do not add).
[ ] Ensure skip semantics are consistent across all sources (manual UI actions, RSS automation, completion-triggered search, webhook `/apply`): unsafe extras ⇒ `skipped` with reason; never add a torrent that will later be deleted for safety.
[ ] Improve logging + run result messages to be actionable:
[ ] - Log “skipped: unsafe piece overlap between content and ignored files” plus a small sample of the boundary condition (e.g., boundary offset and piece length).
[ ] - Keep existing “content file size mismatch” as “rejected” (stronger signal: likely different release/corruption).
[ ] Add/extend tests to lock in behavior (unit tests; no hashing, no qBittorrent needed):
[ ] - Boundary-safe case: ignored extras appended at a piece boundary ⇒ allowed.
[ ] - Boundary-unsafe case: ignored extras start mid-piece (or piece spans content+extra) ⇒ skipped.
[ ] - Multi-transition case: ignored files interleaved between content files ⇒ skipped unless every transition is piece-aligned.
[ ] - Ensure behavior is identical for reuse and hardlink eligibility decisions.
[ ] Update documentation and UI copy to match new rules:
[ ] - `README.md` + `docs/CROSS_SEEDING.md`: explain that ignorePatterns/tolerance can cause missing files to be downloaded, which is only allowed when piece-boundary safe; otherwise cross-seed is skipped (reflinks/copy mode needed for the unsafe cases).
[ ] - `web/src/pages/CrossSeedPage.tsx`: update help text (remove “ignore patterns are reuse-only” wording) and clearly describe the new “piece-boundary safe extras only” limitation.
[ ] Run validation locally: `make test` (and `make lint` for changed files). If OpenAPI schemas/behavioral responses change, run `make test-openapi` and update swagger docs.

## Open questions
- None.
