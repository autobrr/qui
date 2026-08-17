# Automation Cross-Seed Delete Safety Implementation Plan

> **For Codex:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task by task.

**Goal:** Make include-cross-seeds deletion use the same resolved-file grouping in preview, execution, and free-space projection.

**Architecture:** Resolve file identity from each torrent's save path plus its relative file names. Fetch candidate file lists once per automation cycle, then keep the results in the evaluation context so projection does no I/O. Preview and execution use one group resolver, while each free-space rule owns only a small set of hashes it has already counted.

**Tech Stack:** Go 1.22+, qBittorrent service types, testify, existing automations package tests.

---

### Task 1: Compare resolved file paths

**Files:**
- Modify: `internal/services/automations/service_test.go:1304`
- Modify: `internal/services/automations/service.go:4598`

**Step 1: Write the failing regression test**

Extend `TestCrossSeedGroupMembers` with a normal layout variant where the trigger uses save path `/downloads` and file `Season 2/01.mkv`, while the candidate uses save path `/downloads/Season 2` and file `01.mkv`. Assert that both hashes are returned.

Also keep a same-content-path case whose resolved files do not overlap. Assert that only the trigger is returned.

**Step 2: Run the focused test and confirm it fails**

Run:

```bash
go test -race -count=1 ./internal/services/automations -run '^TestCrossSeedGroupMembers$'
```

Expected: the layout-variant case returns only the trigger because the current code compares torrent-relative file names.

**Step 3: Implement resolved-path overlap**

In `service.go`:

- Build a normalized path from `torrent.SavePath` and each qBittorrent file name.
- Store the trigger's file sizes by resolved path and its total bytes in a small index.
- Build that trigger index once in `crossSeedGroupMembers`.
- Scan each candidate file list against the index.
- Count the smaller expected size when both torrents resolve a file to the same disk path.
- Keep the existing 90 percent threshold and unknown-result behavior.
- Rename the `skip` and `files` parameters to describe their purpose.

Do not use host `filepath` rules because the qBittorrent instance can use Windows paths while qui runs on Unix, or the reverse. Use the package's slash-normalizing path helper.

**Step 4: Run the focused test and confirm it passes**

Run the command from Step 2.

**Step 5: Commit**

```bash
git add internal/services/automations/service.go internal/services/automations/service_test.go
git commit -m "fix(automations): resolve cross-seed file paths"
```

### Task 2: Let every direct preview match resolve its own group

**Files:**
- Modify: `internal/services/automations/service_test.go`
- Modify: `internal/services/automations/service.go:1353-1505`

**Step 1: Write the failing preview regression test**

Add a package-level file-fetch function type that can be supplied to the preview helper. In the test, provide two torrents that:

- share one content path;
- both match the delete rule directly;
- have file lists with zero resolved overlap.

Call `previewDeleteIncludeCrossSeeds` with the test fetcher. Assert two direct matches and zero cross-seed matches.

**Step 2: Run the focused test and confirm it fails**

Run:

```bash
go test -race -count=1 ./internal/services/automations -run '^TestPreviewDeleteIncludeCrossSeeds_AllDirectMatches$'
```

Expected: the current processed-content-path guard suppresses the second direct match.

**Step 3: Remove content-path suppression**

In `service.go`:

- Remove `processedContentPaths` and its two methods from `crossSeedExpansionState`.
- Remove the check and mark operations from the preview loop.
- Pass the file-fetch function into the preview expansion helper.
- Have production supply a closure around `GetTorrentFilesBatch`.
- Keep `expandedSet`; it already prevents duplicate output and repeated expansion of a confirmed member.

Do not add fallback behavior. A missing or failed file list still rejects the unsafe group.

**Step 4: Run focused preview and group tests**

Run:

```bash
go test -race -count=1 ./internal/services/automations -run '^(TestPreviewDeleteIncludeCrossSeeds_AllDirectMatches|TestCrossSeedGroupMembers)$'
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/services/automations/service.go internal/services/automations/service_test.go
git commit -m "fix(automations): preview every cross-seed match"
```

### Task 3: Make free-space projection file-aware

**Files:**
- Modify: `internal/services/automations/evaluator.go:38-90,1293-1348`
- Modify: `internal/services/automations/processor.go:878-930`
- Modify: `internal/services/automations/processor_test.go:880-1060`
- Modify: `internal/services/automations/service.go:949-1025,2080-2150`

**Step 1: Write failing projection tests**

Add focused table cases for include-cross-seeds plus `FREE_SPACE`:

1. Two direct matches share a content path but have different resolved files. Each torrent must add its own size, so an unrelated third torrent is no longer selected after the target is reached.
2. Two torrents resolve to the same files. Their shared data must add space once, and a third independent torrent remains necessary to reach the target.

Supply the file lists through the evaluation context. Assert both the chosen hashes and `SpaceToClear`.

**Step 2: Run the focused tests and confirm they fail**

Run:

```bash
go test -race -count=1 ./internal/services/automations -run '^TestProcessTorrents_IncludeCrossSeedsFreeSpaceProjection$'
```

Expected: the old content-path key counts unrelated torrents once, so the first case selects the third torrent incorrectly.

**Step 3: Add cycle and rule projection state**

In `evaluator.go`:

- Add a cycle-local `CrossSeedFilesByHash` map to `EvalContext`.
- Add `CrossSeedHashesToClear` to the current context and `FreeSpaceSourceState`.
- Load and persist the per-rule hash set with the other free-space source state.

**Step 4: Fetch only candidate file lists once**

In `service.go`:

- Detect delete rules that combine include-cross-seeds mode with a `FREE_SPACE` condition.
- From the existing content-path index, collect only hashes in groups of at least two.
- Fetch those file lists in one batch before torrent evaluation.
- Keep any successful partial batch entries. Missing entries cause the existing resolver to skip that group.
- Allocate a confirmed-hash set only for the eligible rules.
- Use the same setup in needed preview.

Do not fetch file lists for unique content paths or for other delete modes.

**Step 5: Resolve cached groups during projection**

In `processor.go`, for include-cross-seeds rules that have the new hash set:

- Return immediately if the trigger hash is already counted.
- Resolve the trigger group with `CrossSeedFilesByHash`; do no client I/O here.
- If resolution fails, add no projected space.
- If it succeeds, add the trigger size once and mark all confirmed members.
- Keep the existing hardlink-signature deduplication after successful group resolution.
- Leave other delete modes on their existing path-based logic.

**Step 6: Run focused package tests**

Run:

```bash
go test -race -count=1 ./internal/services/automations -run '^(TestProcessTorrents_IncludeCrossSeedsFreeSpaceProjection|TestCrossSeedGroupMembers|TestPreviewDeleteIncludeCrossSeeds_AllDirectMatches)$'
```

Expected: PASS.

**Step 7: Commit**

```bash
git add internal/services/automations/evaluator.go internal/services/automations/processor.go internal/services/automations/processor_test.go internal/services/automations/service.go
git commit -m "fix(automations): project cross-seed space by files"
```

### Task 4: Verify the branch

**Files:**
- Review: all files changed since the PR branch base

**Step 1: Format and run required pre-commit checks**

Run:

```bash
make precommit
```

Expected: PASS. If formatting changes files, inspect and commit only relevant changes.

**Step 2: Run the required targeted package tests**

Run:

```bash
go test -race -count=1 ./internal/services/automations
```

Expected: PASS.

**Step 3: Run the required build**

Run:

```bash
make build
```

Expected: PASS.

**Step 4: Inspect the final diff**

Run:

```bash
git status --short
git diff HEAD~3 --check
git diff HEAD~3 --stat
```

Confirm that the implementation remains within automations and its focused tests. Do not push or post on the PR.

**Step 5: Commit check-only edits if needed**

Use a focused conventional commit only if `make precommit` changed relevant files.
