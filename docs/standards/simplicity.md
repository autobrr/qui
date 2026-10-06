# Simplicity standards

These rules apply to Go and TypeScript code.

## Code shape

- Prefer behavior-bearing branches only.
- If multiple `switch` cases equal `default`, collapse them.
- Boolean classifiers should list exceptional `true`/error cases; let `default` handle common path.
- Do not add documentation-only branches unless compiler/linter/tests enforce value.

## Go types

- Group package interfaces by domain under `internal/<area>`.
- Keep interfaces small (<=5 methods).
- Avoid `map[string]interface{}`; use structs.

## Comments

A comment caches what the code cannot show: why this shape, the bug a guard prevents, a coupling to another file. Caching what the line does buys nothing and rots first. One line is the norm.

- Change a line, change its comment, in the same diff. A stale-comment finding from a review bot is right; a docstring coverage percentage is not.
- An invariant a future change must hold is a test, not a sentence with "must not" in it.
- Doc-comment an exported identifier when its name leaves the contract unclear.
