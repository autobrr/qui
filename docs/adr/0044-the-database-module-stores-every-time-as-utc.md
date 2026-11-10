---
status: accepted
date: 2026-11-09
---

# The database module stores every time as UTC

qui stores every time as UTC on SQLite and on Postgres, and reads every time back as UTC. The database module sets this when it opens a connection. The SQLite driver converts each time on write and on read. Each Postgres connection uses the session time zone UTC, and a timestamp codec converts each bound time to UTC. No store converts a time. Issue #3941.

Before this decision, each store had to call `.UTC()` before it bound a time. A store that did not do this saved a local wall clock in a Postgres `TIMESTAMP` column. That value read back shifted by the zone offset. The database clock (`CURRENT_TIMESTAMP`) used the session time zone, which qui did not set.

## Considered options

- **Move the Postgres columns to `TIMESTAMPTZ`.** Rejected. It fixes only Postgres and needs a migration of about 100 columns. The session zone and the codec already make a `TIMESTAMP` column hold a UTC wall clock.
- **Convert the bound arguments where the database module rewrites placeholders.** Rejected. It misses a time inside another type. It also misses the rows of the SQLite-to-Postgres copy, which do not go through that code.
- **Keep the `timezone` parameter of the user's Postgres connection.** Rejected. On a server or a connection that is not in UTC, every database-clock time is shifted. Schedules that read these times run early or late.
- **Rewrite the old rows in a migration.** Rejected. A migration cannot tell which rows the qui process wrote in its zone and which rows the server wrote in its zone.

## Consequences

- qui ignores a `timezone` parameter in the user's Postgres connection string.
- Rows saved before this change stay shifted on Postgres until a store writes them again. A shifted `created_at` stays shifted.
- SQLite needs no data change. The driver reads the stored offset and converts the time to UTC.
- A `forbidigo` rule reports a SQLite or Postgres connection that production code opens outside `internal/database`.
- A contract test runs on both engines with the process zone west of UTC and east of UTC, and with a Postgres session zone that is not UTC.
