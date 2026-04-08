# Contributing

## Database Migrations

Database schema changes must include both SQLite and Postgres migrations.

For an open PR, keep schema work consolidated to at most one new migration file per driver:

- one file under `internal/database/migrations`
- one file under `internal/database/postgres_migrations`

If a PR needs more schema changes before merge, update the draft migration files for that PR instead of adding more migration files. Consolidate before merge.
