---
status: accepted
date: 2026-11-23
---

# A listed effect hook holds one effect that returns a cleanup

A file in `effectHooks` of `web/eslint.config.js` calls one effect, and the function that it passes to the effect returns a cleanup. The ESLint rule `qui/effect-hook-shape` runs on the listed files only. It reports each effect after the first one in a file, and each effect whose function returns nothing. Each message names the fix.

ADR 0073 moved the effects into the listed hooks, and `qui/no-direct-effect` skips those files. So no check read the one place where an effect is allowed. `docs/standards/frontend-review.md` said that a listed hook hides one effect and that the effect cleans up, and only a reviewer applied that sentence. An author who writes a new hook copies a listed one, so a wrong shape there leads to the next one. On 2026-11-23 the five listed hooks pass the rule, so it starts with no entry in `web/eslint-suppressions.json`.

The rule reads the syntax only. An effect returns a cleanup when its first argument is a function that is written at the call, and the body of that function has a `return` with a value. A `return` in a function inside the body does not count.

## Consequences

- A hook that needs two effects for one job writes them as one effect. A second job gets a hook in its own file and a line in `effectHooks`.
- `useEffect(() => subscribe(listener), [])` gets a report, although `subscribe` returns its cleanup. The author writes a block body with `return subscribe(listener)`.
- An effect with nothing to undo needs an `eslint-disable-next-line` comment with its reason, and ADR 0079 holds such an exception.
- The rule does not check that the returned value is a function.
- Nothing checks that a fetch in a listed hook drops a stale response. That sentence stays in `docs/standards/frontend-review.md`.

## Considered options

- **Keep the sentence in `docs/standards/frontend-review.md`.** Rejected. Nothing fails when a change skips it.
- **Run the rule on every file.** Rejected. `qui/no-direct-effect` already reports each effect outside the listed hooks, and each held effect would get a second report.
- **Read the type of the returned value with the TypeScript checker.** Rejected. The ESLint run of `web/` has no type information, and the syntax finds the two shapes that the sentence named.
