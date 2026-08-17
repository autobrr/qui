# Automation Cross-Seed Delete Safety Design

## Goal

Make `deleteWithFilesIncludeCrossSeeds` use file-aware groups in preview, execution, and free-space projection.

The change must preserve these documented rules:

- The needed preview reflects the actual delete run.
- A direct rule match remains eligible when an unrelated torrent shares its content path.
- Confirmed cross-seeds are removed together.
- Confirmed cross-seeds count once in free-space projection.
- Missing or partial file data skips the unsafe group.

## File Identity

qBittorrent file names are relative to each torrent's save path. The same disk file can therefore have different torrent-relative names.

The overlap calculation will compare normalized paths built from each torrent's save path and file name. If two files resolve to the same path, their shared bytes equal the smaller expected size. The existing 90% threshold remains unchanged.

This rule handles a normal qBittorrent layout difference. For example, these values identify the same file:

- Save path `/downloads`, file name `Season 2/01.mkv`
- Save path `/downloads/Season 2`, file name `01.mkv`

Torrents that only share a folder have no resolved file-path overlap. They remain separate direct matches.

## Preview and Execution

The preview will stop marking a full content path as processed. The expanded-hash set already prevents duplicate cross-seed expansion.

Each direct match can therefore resolve its own file group. Execution will continue to use the same group resolver. Preview and execution will apply the same zero-overlap, threshold, partial-overlap, and missing-file rules.

## Free-Space Projection

The service will fetch the required file lists once before it evaluates a free-space rule that uses include-cross-seeds mode. The batch result will remain in the evaluation context for that automation cycle.

Each free-space rule will also keep a set of confirmed cross-seed hashes that already contribute to projected space. The projection code will resolve a trigger with the cached file lists:

- If resolution fails, the group adds no projected space.
- If the trigger is already in the set, it adds no projected space.
- If resolution succeeds, the trigger size is added once and every confirmed member enters the set.
- Zero-overlap members do not enter the set. They can contribute later as independent direct matches.

The existing hardlink signature set continues to deduplicate hardlinked copies across different content paths. Other delete modes keep their current projection behavior.

## Error Handling

The resolver keeps the current safety behavior. A missing list, fetch error, or partial overlap rejects the group. A zero-overlap member remains outside the group.

No retry, fallback deletion, compatibility layer, or new configuration is added.

## Tests

Focused regression tests will cover:

1. Two torrent layouts that resolve to the same disk file.
2. Multiple direct zero-overlap matches under one content path in preview.
3. Free-space projection that counts real cross-seeds once and unrelated matches separately.

Existing threshold, partial-overlap, missing-list, and fetch-error tests remain in place.

## Scope

The change stays in the automations service, evaluation context, processor, and package tests. The user documentation needs no behavior change because it already describes the intended result.
