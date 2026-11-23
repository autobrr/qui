---
status: accepted
date: 2026-11-23
---

# Code outside a feature folder imports only its entry files

A feature folder is a folder directly under `web/src/components`, such as `instances` or `torrents`. The files at the root of a feature folder are its entry files, and its subfolders are its inside. A file outside the feature imports only the entry files. A file of the feature imports each file of the feature. The ESLint rule `qui/entry-files` reports each import from outside into a subfolder, and its message names the fix: move the file that other code needs to the root of its feature.

Before this decision, nothing failed when a page imported a file from three folders down in another feature. Each such import makes a file of the inside a part of the surface of the feature, so a change to it needs a search through the whole app. An author who adds a subfolder cannot keep its files private. On 2026-11-23 the rule found 22 such imports in 14 files: 20 into `instances` and 2 into `torrents`.

The 22 imports are in `web/eslint-suppressions.json`, so the rule is a ratchet, as in ADR 0073. ESLint fails a file that gains such an import, and `scripts/ratchet` fails when the total grows.

## Consequences

- A file that other features need lives at the root of its feature. A feature can have many entry files.
- A file at the root that only re-exports from a subfolder is not a fix. It keeps the file of the inside a part of the surface, under a second name. Nothing checks this yet.
- An import of a subfolder by its name, which loads the `index.ts` in it, counts as an import into the subfolder.
- The tests are outside the rule, as in ADR 0076.
- The folders outside `components`, such as `hooks/torrent-table` and `lib/torrent-table`, are outside the rule. Their only subfolders hold tests.
- A pull request that removes a held import also lowers the count of its file, with `pnpm exec eslint --prune-suppressions <file>`.

## Considered options

- **dependency-cruiser with a rule on the depth of the path.** Rejected. It is a second tool with its own run and its own file of known violations, beside ESLint and `web/eslint-suppressions.json`. The rule is about 40 lines, and it shares the reader of imports with `qui/layer-order`.
- **`no-restricted-imports` with a pattern for the subfolders.** Rejected. A pattern cannot allow the files of the same feature, and ADR 0076 names a second problem with one more block of that rule.
- **Move the 22 imports in the same change.** Rejected. 20 of them need a decision on which files of `instances` are its surface.
