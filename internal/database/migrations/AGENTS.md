# Database Migrations

## How Migrations Work

The migration system executes the **entire file content** as SQL via `tx.ExecContext()`. It does NOT parse or understand `-- +migrate Up/Down` markers - these are just SQL comments that SQLite ignores.

**Everything that is valid SQL in the file WILL be executed.**

## Writing Migrations

Just write the SQL statements needed. No special markers required.

```sql
-- Adding a column
ALTER TABLE users ADD COLUMN email TEXT;

-- Creating a table
CREATE TABLE sessions (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL
);
```

## Common Mistakes

### DO NOT include Down migrations with actual SQL

This is **WRONG** - the DROP TABLE will execute immediately after CREATE:

```sql
-- +migrate Up
CREATE TABLE foo (id INTEGER PRIMARY KEY);

-- +migrate Down
DROP TABLE foo;
```

Result: Table is created, then immediately dropped.

### If you want Down documentation, comment it out entirely

```sql
ALTER TABLE foo ADD COLUMN bar TEXT;

-- Down migration (manual):
-- ALTER TABLE foo DROP COLUMN bar;
```

## Safety Tips

- Use `IF NOT EXISTS` / `IF EXISTS` when appropriate
- Test migrations on a copy of your database before committing
- Remember: once a migration runs in production, the filename is recorded and it won't run again

## String Pool References

When adding a new table with columns that reference `string_pool(id)`, you **MUST** also update the `referencedStringsInsertQuery` in `internal/database/db.go`.

This query is used by `CleanupUnusedStrings()` to identify which strings are still in use. If your new table's FK columns are missing from this query, the cleanup will attempt to delete strings that are still referenced, causing:
1. Foreign key constraint failures (error 787)
2. Transaction leaks (SQLite keeps the transaction active after deferred FK check failures)

### Example

If you add a migration like:
```sql
CREATE TABLE my_table (
    id INTEGER PRIMARY KEY,
    name_id INTEGER NOT NULL REFERENCES string_pool(id),
    url_id INTEGER REFERENCES string_pool(id)
);
```

You must add to `referencedStringsInsertQuery` in `db.go`:
```sql
UNION ALL
SELECT name_id AS string_id FROM my_table WHERE name_id IS NOT NULL
UNION ALL
SELECT url_id AS string_id FROM my_table WHERE url_id IS NOT NULL
```
