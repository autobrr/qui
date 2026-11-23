# Simplicity standards

These rules apply to Go and TypeScript code.

## Code shape

- Prefer behavior-bearing branches only.
- If multiple `switch` cases equal `default`, collapse them.
- Boolean classifiers should list exceptional `true`/error cases; let `default` handle common path.
- Do not add documentation-only branches unless compiler/linter/tests enforce value.

## Libraries

- Before you write logic that a published spec or format defines (Markdown, HTML, URLs, CSV, semver, cron, time zones), use a well-proven library for it. Name the library in the PR body. If hand-written logic of this kind needs a second fix for an edge case, replace it with the library.

## Go types

- Group package interfaces by domain under `internal/<area>`.
- Keep interfaces small (<=5 methods).

## Types and boundaries

- Parse external data where it enters qui: a request body, a response from qBittorrent or a tracker, a config file, a file name from a torrent. After that point, trust the type, and do not check the value again.
- Make an illegal state impossible to build. One value with named cases is better than two flags that must not be true at the same time, as the `useReducer` rule in `frontend-review.md` asks.
- Give values that share a raw type but mean different things a type each, so that the compiler catches a mix-up: `models.PermissionArea` and `models.PermissionLevel` are both strings. `paths.md` keeps host paths and backend paths apart, and the `mapany` ratchet ("Maps of Any" in `docs/linting.md`) asks for a struct, not a `map[string]any`.

## Comments

Comments are banned in Go and TypeScript code. `make lint-ratchet` counts them in Go, and the ESLint rule `qui/no-comments` reports them in `web/`. "Ratchet" in `docs/linting.md` describes both checks.

- Let a name, a type, or a test say what the comment would say. An invariant that a future change must hold is a test.
- Record a decision that is hard to reverse, surprising, and the result of a real trade-off as an ADR in `docs/adr/`.
- Open an issue for a shortcut that the code takes. A comment that excuses the shortcut keeps it in the code.
- Put the history of an edit in the commit message.

The markers that a tool or a standard in this repo reads stay, because they are not prose:

- Go: the license header, `//go:` directives, `//nolint:<linter> // reason`, `//exhaustive:`, `// #nosec`, `// Deprecated:`, and `// regression: #N`.
- TypeScript: the license header, `eslint-disable` and `eslint-enable`, `@ts-expect-error`, `@ts-nocheck`, `/// <reference`, `@deprecated`, `// i18n-namespace: <ns>`, and `// regression: #N`.

Comments from before the ban stay until `/delete-comment` reaches their file. The number in a file can only go down:

- When you change a line that has a comment, delete the comment, or update it if it is still true and you cannot yet move it.
- A stale-comment finding from a review bot is right.
- Older comments are no reason to add more.
