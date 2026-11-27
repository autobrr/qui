---
status: accepted
date: 2026-11-25
---

# A UI primitive owns its look, and `@shadcn/lint` checks it one primitive at a time

New UI drifted from the look of the pages, and the rule to copy a page caught nothing. qui adopts `@shadcn/lint` to fail a class value that lint cannot read (`require-static-classes`), a class that Tailwind cannot generate (`no-unknown-classes`), an arbitrary value outside layout (`no-arbitrary-values`), a class that restyles a UI primitive (`no-restyle`), and a raw palette color (`no-raw-colors`). Existing findings are held by count in `web/eslint-suppressions.json`, as in ADR 0073. Each rule turns on once its options exist: `require-static-classes` first, `no-restyle` one primitive at a time starting with Button, Badge, and Label, and `no-raw-colors` once theme status colors land.

## Consequences

- A page passes layout classes to a UI primitive and nothing else, and picks its look with an option (a variant or a size).
- A new option of a UI primitive needs a maintainer. `settings.shadcn.note` appends that reminder to each lint message.
- A held count in `web/eslint-suppressions.json` lets one finding replace another in the same file. Reviewers read the touched lines of files with held findings.
- The plugin is pinned at version `0.2.5` and calls an API of Tailwind that Tailwind marks as unstable, so it is excluded from Dependabot's monthly npm group. Updates arrive in a standalone PR where a maintainer records any count changes.
- CI fails the frontend lint step if `@shadcn/lint` writes a warning on stderr (for example when `components.json` cannot be read, or when Tailwind or the theme stylesheet fails to load under rules that read the stylesheet).

## Considered options

- **Rules of qui's own.** Rejected. `@shadcn/lint` already knows the class grammar of Tailwind and reads the options of each UI primitive.
- **Prose rules alone.** Rejected. Nine maintainer review corrections showed that prose rules do not hold.
- **Turn on `no-restyle` for all primitives at once.** Rejected. In a replay of the 77 web changes from 2026-11-14 to 2026-11-25, failing changes had 7 new findings in the median case and up to 60, mostly on primitives without options to use, versus 4 and up to 25 with three primitives.
- **`no-inline-styles`.** Rejected for now. Half of its 246 findings are computed positions and widths in the torrent list.
