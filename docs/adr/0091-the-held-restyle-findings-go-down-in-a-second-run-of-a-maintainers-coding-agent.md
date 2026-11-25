---
status: accepted
date: 2026-11-25
---

# The held restyle findings go down in a second run of a maintainer's coding agent

The run of ADR 0084 for the held effects gets a sibling for the findings that `@shadcn/lint` holds in `web/eslint-suppressions.json`. It has its own label and its own single open pull request, takes one file with a cap on its elements, shows before and after pictures, may change a UI primitive when the fix needs it, and skips a file with no safe fix. Nothing in the repository starts it.

## Consequences

- Each run starts from an updated `develop`, because both runs edit `web/eslint-suppressions.json`.
- One pull request takes one file, shows before and after pictures, and waits for a maintainer to merge.
- No CI workflow starts the run; it runs on a maintainer's machine once the status color tokens and `no-raw-colors` land (issue #4522).

## Considered options

- **One run for both held effects and held restyle findings.** Rejected. A single open pull request cap would make each stream wait for the other.
- **A CI lane.** Rejected for the reasons of ADR 0084: CI has no browser to capture before and after pictures, and bot pull requests get the weakest review for a visual change.
