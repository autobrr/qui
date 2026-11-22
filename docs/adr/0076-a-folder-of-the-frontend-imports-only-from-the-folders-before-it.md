---
status: accepted
date: 2026-11-22
---

# A folder of the frontend imports only from the folders before it

The folders of `web/src` have an order: `types`, `utils`, `config`, `i18n`, `lib`, `contexts`, `hooks`, `components`, `layouts`, `pages`, `routes`. A file imports from its own folder and from the folders before it. `layers` in `web/eslint.config.js` holds the order. The ESLint rule `qui/layer-order` reports each import against it, and its message names the three fixes: move the imported code down, move the file up, or take the value as a parameter.

Before this decision, nothing failed when a helper in `lib` imported a component. Such an import lets two folders import each other, and a change to a screen can then break a helper that has no screen. An author who reads one file sees only the imports of that file, so the order held only where each author knew it. On 2026-11-22 the rule found 28 imports against the order in 21 files, of about 1,800 imports between the folders.

The 28 imports are in `web/eslint-suppressions.json`, so the rule is a ratchet, as in ADR 0073. ESLint fails a file that gains an import against the order, and `scripts/ratchet` fails when the total grows.

## Consequences

- Code that a lower folder and a higher folder both need lives in the lower one. A type that `lib` and a component share lives in `types` or `lib`, not next to the component.
- The files at the root of `web/src`, such as `router.tsx`, and the folders `demo` and `test` are outside the order. They import from each folder, and the rule does not report an import of them.
- The tests are outside the rule. A test of a helper can render a component or call a hook to set up its case.
- A new folder under `web/src` is outside the order until a change adds it to `layers`.
- A pull request that removes an import against the order also lowers the count of its file, with `pnpm exec eslint --prune-suppressions <file>`.

## Considered options

- **A rule in `docs/standards/frontend-review.md`.** Rejected. Nothing fails when a change skips it.
- **`no-restricted-imports` with one block for each folder.** Rejected. In a flat config, the last block that matches a file replaces the options of the same rule in earlier blocks, and `web/eslint.config.js` already uses the rule for other bans. Each folder block would turn those bans off for its files.
- **A boundaries plugin from npm.** Rejected for now. The rule is about 50 lines with no dependency. A plugin is the better choice if the feature folders under `components` get an order of their own.
- **Fix the 28 imports in the same change.** Rejected. Each one needs a decision on where the shared code belongs.
