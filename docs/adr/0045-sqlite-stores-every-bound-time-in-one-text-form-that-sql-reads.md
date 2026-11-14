---
status: accepted
date: 2026-11-09
---

# SQLite stores every bound time in one text form that SQL reads

On SQLite, the database module makes the driver write every bound time in the form `2006-01-02 15:04:05.999999999-07:00`. Because of ADR 0044, the stored time is UTC, for example `2026-11-09 11:41:31.947225+00:00`. One migration rewrote the older text forms in every time column to this form. Each SQLite time column now holds only this form or the database clock form (`2026-11-09 11:41:31`). SQL time functions read both forms, and text comparison gives the order of the instants. Stores bind a plain time on both engines and never format a time as text. SQLite returns a computed time column, such as MIN or MAX, as text, so stores scan it into one nullable time type in `internal/dbinterface`, which reads both forms. Issue #3945.

Before this decision, the driver wrote a bound time in the Go text form, `2026-11-09 11:41:31.947225572 +0000 UTC`. `julianday()` and `datetime()` return NULL for that form. Rows written before ADR 0044 on a host outside UTC also held a local offset, and they sorted wrongly as text. Stores formatted cutoffs as special text and parsed several forms to work around this.

## Considered options

- **The database clock form (`_time_format=datetime`).** Rejected. It drops the sub-seconds, and the partial pool compares its admission stamp, in microseconds, by equality.
- **A form that qui defines itself.** Rejected. The driver offers only three named forms. Any other form needs qui to convert each bound argument, which ADR 0044 rejected.
- **Let old rows age out.** Rejected. The partial pool compares its stored admission stamp by equality. A member that qui admitted before the upgrade cannot be claimed, moved, or removed until qui admits it again, and no error shows.
- **The driver option `_texttotime` for computed columns.** Rejected. It parses all computed text that looks like a date, so a saved search for `2026-11-09` comes back as `2026-11-09T00:00:00Z`. It applies to the whole connection.
- **Rewrite only the partial pool columns.** Rejected. Rows that never expire, such as API keys, instances, and backups, would keep the old form forever. SQL time functions would return NULL for them, and rows with a local offset would keep sorting wrongly.

## Consequences

- The first start after the upgrade reads each SQLite table with a time column one time, and writes each row that holds the old form again. Each row of the file cache holds the old form, so this start took 8.6 seconds longer in a test with 2 million cached files.
- When the driver reads a stored time, it tries the Go text form first. A read of a bound time costs about 0.2 microseconds more than before.
- If a user goes back to v1.35.0 or earlier, the old version binds the Go text form again. Partial pools that are in progress then stop matching their admission stamps.
- A test fails when the migration misses a time column of the schema at that migration.
- A contract test pins the text form that the driver writes for a bound time.
