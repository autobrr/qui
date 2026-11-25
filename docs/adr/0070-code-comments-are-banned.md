---
status: accepted
date: 2026-11-20
---

# Code comments are banned

Go and TypeScript code in qui carries no prose comments. A name, a type, or a test says what the code does. A decision goes into an ADR, and a shortcut goes into an issue.

A comment that excuses a shortcut lets the shortcut stay, and an agent that reads the file copies the pattern into the next change. A comment that repeats the code costs a line, and no check fails when it goes stale. The first audit of 4,424 comment blocks found 2,661 that repeated the code and 59 that were stale.

The markers that a tool or a standard reads are not prose and stay: the license header, compiler and linter directives, `// regression: #N`, and `// i18n-namespace:`.

About 39,000 comments were in the code when the ban started. On 2026-11-25 one change deleted every comment that was left, without sorting them, and with them the counts that held them. Commit `9fcce7861` still has the text. `scripts/ratchet` fails on each comment in a Go file, and ESLint fails on each comment in a web file. No allowlist holds one.

## Consequences

- A fact that the code cannot show, such as a quirk of qBittorrent or of a filesystem, goes in a name, a test, or an ADR. The first audit found 1,670 blocks of that kind. Those that had no other home are only in the git history.
- An open pull request that conflicts with the removal keeps its code and drops the comments.

## Considered options

- **Keep the rule "default to no comment".** Rejected. That rule was in `docs/standards/simplicity.md` and left the choice to the author of each change. The first audit found the 2,661 and 59 blocks above under it.
- **Sort each old comment before it goes.** Rejected on 2026-11-25. The first five days of the ban did this with a skill that moved decisions to ADRs and shortcuts to issues, and kept each block that held a fact. With that sort, the comments never all go.
- **Delete the old comments only on the code that a change touches.** Rejected on 2026-11-25. A check did this for Go. It needed a count for each file and a rule for each way that code can move, and the comments on code that nobody changes would stay.
