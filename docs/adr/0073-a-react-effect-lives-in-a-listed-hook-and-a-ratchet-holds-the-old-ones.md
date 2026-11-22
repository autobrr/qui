---
status: accepted
date: 2026-11-22
---

# A React effect lives in a listed hook, and a ratchet holds the old ones

A component in `web/src` calls no `useEffect`, `useLayoutEffect`, or `useInsertionEffect`. An effect lives in a hook that hides it behind one named job, such as `useDebounce` or `useNow`. `effectHooks` in `web/eslint.config.js` lists each of these hooks with its job. The ESLint rule `qui/no-direct-effect` reports every other call outside the tests, and its message names what to write instead.

An effect is right only with the right dependencies, a cleanup, and a job that render cannot do, and nothing fails when one of the three is missing. An author who reads one file copies the nearest example, so each effect in a component leads to the next one. On 2026-11-22 the rule found 226 calls in 106 files. `react-hooks/set-state-in-effect` reports 60 of them, because they set state in the body of the effect.

The 226 calls are in `web/eslint-suppressions.json`, so the ban is a ratchet, as in ADR 0070. ESLint fails a file that gains an effect, and `scripts/ratchet` fails when the total grows.

## Consequences

- A job that needs a new effect costs a new hook and a line in `effectHooks`, and a reviewer sees both in the diff.
- A pull request that removes an effect also lowers the count of its file, with `pnpm exec eslint --prune-suppressions <file>`.
- `scripts/ratchet` fails on an entry in `web/eslint-suppressions.json` for any rule that `suppressedRules` in `scripts/ratchet/comments.go` does not list. Before this decision, `eslint --suppress-rule` could hide the reports of any rule there.
- `scripts/ratchet` also fails when a rule in `suppressedRules` has no entry left. The change that removes the last effect deletes `qui/no-direct-effect` from the list, so a later change cannot add effects to the file again without a change to the list.

## Considered options

- **Keep the rule "use `useEffect` only to sync with external systems" in `docs/standards/frontend-review.md`.** Rejected. Nothing fails when a change skips it.
- **Allow an effect in every file under `web/src/hooks/`.** Rejected. A change could then move each effect into a hook of its own and pass, with the effect as it was.
- **Rewrite the 226 calls in one change.** Rejected. A reader must decide for each one what replaces it: a value from render, a `key`, a handler, a store, or a listed hook.
