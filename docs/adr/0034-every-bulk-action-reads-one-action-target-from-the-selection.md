---
status: accepted
date: 2026-10-29
---

# Every bulk action reads one Action target from the selection

One pure function in the selection library turns the selection, the scope, and the remembered rows into the Action target. The desktop table, the header bar, and the mobile cards call it, and export, field copy, and the cross-seed warning read its select-all part. Before this decision, each surface built the request itself, so a fix reached one layout only (#1926, #2779). Issue #3481.

Each bulk action handler takes the action value and the Action target. A new surface cannot build a select-all request by hand, because the type check fails.

The table menu keeps no copy of the torrents it acts on. Since #3368, a right-click on a row outside the selection selects only that row before the menu opens. A copy that differs from the selection can make a delete reach torrents that the user did not pick.

## Considered options

- **A hook that reads the selection context.** Rejected: the table and the cards get their rows in different ways, so the hook needs both ways passed in.
- **A lint rule on the helper that builds instance-and-hash pairs.** Rejected: the typed handlers block the real risk, and the helper is private.

## Consequences

- A new bulk action surface gets its target from the function. It does not read the selection keys to build a request.
- A dialog that keeps its own copy of the selected torrents reverses this decision and needs a new ADR.
