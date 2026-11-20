---
status: accepted
date: 2026-11-20
---

# Code comments are banned, and a ratchet holds the old ones

Go and TypeScript code in qui carries no prose comments. A name, a type, or a test says what the code does. A decision goes into an ADR, and a shortcut goes into an issue.

A comment that excuses a shortcut lets the shortcut stay, and an agent that reads the file copies the pattern into the next change. A comment that repeats the code costs a line, and no check fails when it goes stale. The first audit of 4,424 comment blocks found 2,661 that repeated the code and 59 that were stale.

The markers that a tool or a standard reads are not prose and stay: the license header, compiler and linter directives, `// regression: #N`, and `// i18n-namespace:`.

About 39,000 comments were in the code when the ban started, so the ban is a ratchet. `scripts/ratchet` counts the comments of each Go file against `scripts/ratchet/allowlist.json`, and ESLint counts the comments of each web file against `web/eslint-suppressions.json`. A file cannot gain a comment, and the totals cannot grow. The `/delete-comment` skill sorts the old comments and deletes them in batches.

## Consequences

- A fact that the code cannot show, such as a quirk of qBittorrent or of a filesystem, has no place in a new file except a name, a test, or an ADR. The audit kept 1,670 blocks of that kind in place, and the ratchet holds them until each one moves.
- A pull request that deletes a comment also lowers the count of its file.

## Considered options

- **Keep the rule "default to no comment".** Rejected. That rule was in `docs/standards/simplicity.md` and left the choice to the author of each change. The first audit found the 2,661 and 59 blocks above under it.
- **Delete every comment in one change.** Rejected. A comment can record a decision or excuse a shortcut, and a reader must sort each one before it goes.
