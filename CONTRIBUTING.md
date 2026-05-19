# Contributing

## Database Migrations

Database schema changes must include:

- a SQLite migration under `internal/database/migrations`
- a Postgres migration under `internal/database/postgres_migrations`
- matching model/store updates in the same PR so schema and code stay in sync

For an open PR, keep schema work consolidated to at most one new migration file per driver:

- one file under `internal/database/migrations`
- one file under `internal/database/postgres_migrations`

If a PR needs more schema changes before merge, update the draft migration files for that PR instead of adding more migration files. Consolidate before merge.

Do not land schema-only changes. The corresponding model/store files touched by the schema change must be updated in the same PR.
