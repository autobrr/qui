# AGENTS.md

Schema and migration rules for work under `internal/database/`.

- Put a change to a migration that already exists on `develop` in a new migration. Migrations are tracked by file name only, so installs that ran the old SQL never run the new SQL.
- When you rename a migration that already exists on `develop`, add an old -> new entry to the matching `*MigrationFilenameRenames` table in `db.go`. Without the entry, the renamed file runs again.
- DB schema changes need SQLite + Postgres migrations, matching model/store updates, same PR.
- Open PRs: consolidate schema work to at most one new SQLite migration and one new Postgres migration; edit draft migrations before merge.
- New `string_pool` FK columns need a leading index in BOTH the SQLite and Postgres migrations, plus an entry in `referencedStringsInsertQuery`. Neither engine auto-indexes FK child columns, and the daily string_pool GC full-scans unindexed ones (see `TestStringPoolFKColumnsAreIndexed` in `db_test.go`). That test enforces this on SQLite; the Postgres index is on you.

## Code Review Rules

- Treat a change to the SQL of a migration that already exists on `develop`, or a rename of one, as P1. Migrations are tracked by file name, so installs that already ran it never run the new SQL. A rename with an entry in the `*MigrationFilenameRenames` tables in `db.go` is safe. Otherwise the fix is a new migration.
