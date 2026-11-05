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
- Avoid `map[string]interface{}`; use structs.

## Comments

Default to no comment. Add one line only for a constraint the code cannot show: a hidden constraint, a workaround (link the issue or upstream bug), or a link to another file.

- Delete a comment that restates the code, such as step narration, section banners, or the function name above the function.
- Put the history of an edit in the commit message.
- Leave out commented-out code and unprompted `TODO` or `FIXME` markers.
- Older comments are no reason to add more.
- When you change a line that has a comment, update or delete the comment.
- A stale-comment finding from a review bot is right.
- An invariant that a future change must hold is a test.

The markers that a standard in this repo requires stay, for example `// regression: #N` and `// i18n-namespace: <ns>`.
