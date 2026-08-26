---
sidebar_position: 5
title: CLI Commands
---

# CLI Commands

## Generate Configuration File

Create a default configuration file without starting the server:

```bash
# Generate config in OS-specific default location
./qui generate-config

# Generate config in custom directory
./qui generate-config --config-dir /path/to/config/

# Generate config with custom filename
./qui generate-config --config-dir /path/to/myconfig.toml
```

## User Management

Create and manage user accounts from the command line:

```bash
# Create initial user account
./qui create-user --username admin --password mypassword

# Create user with prompts (secure password input)
./qui create-user --username admin

# Change password for existing user (no old password required)
./qui change-password --username admin --new-password mynewpassword

# Change password with secure prompt
./qui change-password --username admin

# Pipe passwords for scripting (works with both commands)
echo "mypassword" | ./qui create-user --username admin
echo "newpassword" | ./qui change-password --username admin
printf "password" | ./qui change-password --username admin
./qui change-password --username admin < password.txt

# All commands support custom config/data directories
./qui create-user --config-dir /path/to/config/ --username admin
```

### Notes

- Only one user account is allowed in the system
- Passwords must be at least 8 characters long
- Interactive prompts use secure input (passwords are masked)
- Supports piped input for automation and scripting
- Commands will create the database if it doesn't exist
- No password confirmation required - perfect for automation

### Reset a Forgotten Password {#reset-password}

If you've forgotten your password, use the `change-password` command to set a new one. No old password is required.

**Linux / macOS:**

```bash
./qui change-password --username admin --new-password mynewpassword
```

**Windows (Command Prompt):**

Navigate to the folder containing `qui.exe` and run:

```batch
qui.exe change-password --username admin --new-password mynewpassword
```

**Docker:**

```bash
docker exec -it <container-name> qui change-password --username admin --new-password mynewpassword
```

Replace `admin` with your username and `mynewpassword` with your desired password (minimum 8 characters).

## Update Command

Keep your qui installation up-to-date:

```bash
# Update to the latest version
./qui update
```

## Command Line Flags

```bash
# Specify config directory (config.toml will be created inside)
./qui serve --config-dir /path/to/config/

# Specify data directory for database and other data files
./qui serve --data-dir /path/to/data/
```

## Migrate From Other Torrent Clients

Import torrents with their state from Deluge, rTorrent or Transmission into qBittorrent's `BT_backup` directory. See [Client Migration](../features/client-migration.md) for per-client details and what gets preserved.

```bash
# Preview what would be imported, without writing anything
./qui migrate transmission \
  --source-dir ~/.config/transmission-daemon \
  --qbit-dir ~/.local/share/qBittorrent/BT_backup \
  --dry-run

# Deluge: point at the state dir inside the Deluge config dir
./qui migrate deluge \
  --source-dir ~/.config/deluge/state \
  --qbit-dir ~/.local/share/qBittorrent/BT_backup

# rTorrent: point at the session dir from your .rtorrent.rc
./qui migrate rtorrent \
  --source-dir ~/.sessions \
  --qbit-dir ~/.local/share/qBittorrent/BT_backup

# Skip the automatic tar.gz backup of both directories
./qui migrate transmission --source-dir ... --qbit-dir ... --skip-backup
```

Notes:

- Stop the source client and qBittorrent before migrating; start qBittorrent afterwards and it will pick up the imported torrents.
- Only fully downloaded torrents are imported. Partial torrents are skipped with a warning so no incorrect piece state ever reaches qBittorrent.
- Preserved per torrent: save path, trackers, upload/download totals, added/completed timestamps, seeding time, paused state, Transmission labels (as qBittorrent tags), Deluge and ruTorrent labels (as the qBittorrent category).
- Supported source versions: Transmission 2.4-4.x, Deluge 1.3.x and 2.x, rTorrent 0.9.x and newer.
- Unless `--skip-backup` is set, both directories are archived to `qbt_backup/` in the current working directory first. The qBittorrent directory is archived only when it already exists; a fresh destination produces only the source archive.
- Torrents already present in the target `BT_backup` are skipped, so re-running is safe.

## Database Migration

Offline SQLite to Postgres migration:

```bash
# 0) Stop qui first (no writes during migration)
#    (example) docker compose stop qui

# 1) Create the target Postgres database first (required)
#    (example) createdb -h localhost -p 5432 -U user qui
#    (or in psql) CREATE DATABASE qui;

# 2) Optional: backup the SQLite file
cp /path/to/qui.db /path/to/qui.db.bak

# 3) Validate source + destination without importing rows
./qui db migrate \
  --from-sqlite /path/to/qui.db \
  --to-postgres "postgres://user:pass@localhost:5432/qui?sslmode=disable" \
  --dry-run

# 4) Apply migration (schema bootstrap + table copy + identity reset)
./qui db migrate \
  --from-sqlite /path/to/qui.db \
  --to-postgres "postgres://user:pass@localhost:5432/qui?sslmode=disable" \
  --apply

# 5) Point qui at Postgres and start it again
#    - config.toml: databaseEngine=postgres + databaseDsn=...
#    - or env: QUI__DATABASE_ENGINE=postgres + QUI__DATABASE_DSN=...
```

Notes:

- Run this while qui is stopped.
- Create the target Postgres database before running migration.
- `--dry-run` and `--apply` are mutually exclusive.
- The command copies all runtime tables except migration history.
- The migrator bootstraps schema/tables inside the destination DB, but does not create the database itself.
- The output includes per-table row counts for SQLite and Postgres.

### FAQ

**Q: Why is `cross_seed_feed_items` row count lower in Postgres after migration?**

This is expected when the SQLite file contains historical rows whose `indexer_id` no longer exists in `torznab_indexers`.
Postgres enforces the foreign key strictly, so migration keeps only rows that still have valid parent records.

You can verify this in SQLite:

```sql
SELECT COUNT(*) AS orphaned_rows
FROM cross_seed_feed_items f
LEFT JOIN torznab_indexers i ON i.id = f.indexer_id
WHERE i.id IS NULL;
```

If `orphaned_rows` matches the migration delta (`sqlite_count - postgres_count`), migration behavior is working as intended.
