---
status: accepted
date: 2026-11-20
---

# String interning reads before it writes

`dbinterface.InternStrings` turns strings into `string_pool` IDs. It first selects the IDs of the values that exist, and inserts only the values that are missing.

The shorter form is one `INSERT ... ON CONFLICT DO NOTHING` for all values. On Postgres, that statement takes the next sequence value before it finds the conflict, so each value that already exists uses up one ID. The file cache interns its whole working set again on each refresh, so the short form would use up the ID space of `string_pool` over time.

The missing values are inserted in sorted order. Two transactions that intern overlapping values then take their row locks in the same order.

## Considered options

- **One upsert for all values.** Rejected for the sequence use on Postgres. SQLite and Postgres run the same code.
