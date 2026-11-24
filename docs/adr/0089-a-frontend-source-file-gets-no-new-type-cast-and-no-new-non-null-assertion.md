---
status: accepted
date: 2026-11-24
---

# A frontend source file gets no new type cast and no new non-null assertion

A type cast (`value as Type`) and a non-null assertion (`value!`) tell the compiler what a value is, and the compiler does not check the claim. In the source files of `web/src`, ESLint reports both. `@typescript-eslint/consistent-type-assertions` with `assertionStyle: 'never'` reports each cast, and `@typescript-eslint/no-non-null-assertion` reports each `!`. `as const` and `satisfies` stay allowed, because the compiler checks them.

Before this decision, `@typescript-eslint/no-explicit-any` was the only rule about types, and a cast passed it. A cast hides the same mistake as `any`. When the shape of an API response or a prop changes, the compiler reports each line that reads the old shape, but not a line behind a cast, and that line fails in the browser. A cast is also the shortest way to make a type error go away, so a coding agent adds one when the types do not fit, and nothing fails. On 2026-11-24 the rules found 371 casts in 175 source files and 86 non-null assertions in 40.

The 371 and the 86 are in `web/eslint-suppressions.json`, so both rules are ratchets, as in ADR 0073. ESLint fails a file that gains a cast or a `!`, and `scripts/ratchet` fails when a total grows.

## Consequences

- Code that needs a narrower type gets it from a check that runs: a type guard (`value is Type`), a zod schema where data enters the app, or a type on the source of the value.
- Where the code is right and the compiler cannot know it, for example at a DOM API or a library with a loose type, the line keeps its cast under `eslint-disable-next-line <rule> -- <reason>`. This is an exception as in ADR 0079, so the pull request waits for a maintainer to set `exceptions-approved`.
- No file is exempt by name. A cast in a shared helper is as unchecked as a cast in a component.
- The tests and `src/test` are outside both rules. A test builds partial objects and mocks, and a wrong cast there fails the test, not the app.
- A cast through `unknown` (`value as unknown as Type`) is two reports.
- A pull request that removes a held cast or `!` also lowers the count of its file, with `pnpm exec eslint --prune-suppressions <file>`.

## Considered options

- **A list of helper files that may cast.** Rejected. The list needs an owner and grows by one line at a time. The exception path of ADR 0079 already shows each new cast to a maintainer, with its reason.
- **The same rules in the tests.** Rejected. On 2026-11-24 the tests had 720 casts and 248 non-null assertions, most of them on partial fixtures and mocks, and a wrong one fails only its test.
- **`@typescript-eslint/no-unsafe-type-assertion`, which reports only a cast to a narrower type.** Rejected for now. It needs lint with type information, which `web/eslint.config.js` does not load, and it lets a cast through `unknown` pass.
- **Fix the 371 casts in the same change.** Rejected. Each cast needs its own choice of a guard, a schema, or a type, in 175 files.
