---
sidebar_position: 5
title: CLI Commands
description: CLI commands for users, updates, migration, and the database.
---

# CLI Commands

## serve

Start the server:

```bash
./qui serve

# Custom config directory (config.toml is created inside)
./qui serve --config-dir /path/to/config/

# Custom data directory for the database and other data files
./qui serve --data-dir /path/to/data/

# Log to a file instead of stdout
./qui serve --log-path /path/to/qui.log

# Enable the pprof server (default 127.0.0.1:6060, override with QUI__PPROF_ADDR)
./qui serve --pprof
```

## version

Print the version of qui:

```bash
./qui version
```

## generate-config

Create a default configuration file without starting the server:

```bash
# Generate config in the OS-specific default location
./qui generate-config

# Generate config in a custom directory
./qui generate-config --config-dir /path/to/config/

# Generate config with a custom filename
./qui generate-config --config-dir /path/to/myconfig.toml
```

If the file already exists, the command keeps it and does not overwrite it.

## create-user and change-password

Create and manage the user account from the command line:

For normal use, omit the password flag. qui prompts for the password and masks your input. Your shell can save `--password` or `--new-password` values in its history. Other users on the system may see those values in the process list.

```bash
# Create a user with a masked password prompt
./qui create-user --username admin

# Change the password with a masked prompt
./qui change-password --username admin

# Both commands accept password flags
./qui create-user --username admin --password mypassword
./qui change-password --username admin --new-password mynewpassword

# Pipe passwords for scripting (works with both commands)
echo "mypassword" | ./qui create-user --username admin
echo "newpassword" | ./qui change-password --username admin
printf "password" | ./qui change-password --username admin
./qui change-password --username admin < password.txt

# Both commands accept custom config/data directories
./qui create-user --config-dir /path/to/config/ --username admin
```

### Notes

- The system allows only one user account.
- Passwords must be at least 8 characters long.
- Interactive prompts mask the password.
- Both commands accept piped input for automation and scripting.
- A piped password must not contain spaces. Type passwords with spaces at the interactive prompt.
- If the database does not exist, `create-user` creates it. `change-password` requires an existing database.
- The commands do not ask you to confirm the password.

### Reset a forgotten password {#reset-password}

If you forgot your password, set a new one with the `change-password` command. The command does not ask for the old password.

**Linux / macOS:**

```bash
./qui change-password --username admin
```

**Windows (Command Prompt):**

Open the folder that contains `qui.exe` and run:

```batch
qui.exe change-password --username admin
```

**Docker:**

```bash
docker exec -it <container-name> qui change-password --username admin
```

Replace `admin` with your username. Enter a new password of at least 8 characters at the prompt.

## update

Update qui to the latest release:

```bash
./qui update
```

This command replaces the qui binary in place. For Docker, pull a new image instead (see [Docker](../getting-started/docker.md#updating)).

## Migrate From Other Torrent Clients

Import torrents with their state from Deluge, rTorrent, or Transmission into qBittorrent's `BT_backup` directory. See [Client Migration](../features/client-migration.md) for per-client details and what qui preserves.

```bash
# Preview what the import would do, without writing anything
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

- Stop the source client and qBittorrent before you migrate. Start qBittorrent afterwards, and it picks up the imported torrents.
- qui imports only fully downloaded torrents. It skips partial torrents with a warning, so no incorrect piece state reaches qBittorrent.
- qui preserves these fields per torrent: save path, trackers, upload/download totals, added/completed timestamps, seeding time, paused state (Deluge 2.x only; Deluge 1.3.x imports start resumed), Transmission labels (as qBittorrent tags), Deluge and ruTorrent labels (as the qBittorrent category).
- qui supports these source versions: Transmission 2.4-4.x, Deluge 1.3.x and 2.x, rTorrent 0.9.x through 0.16.x.
- If you do not set `--skip-backup`, qui first archives both directories to `qbt_backup/` in the current working directory. If the qBittorrent directory already exists, qui archives it. A fresh destination produces only the source archive.
- If a torrent already exists in the target `BT_backup`, qui skips it. You can run the command again.

## Database migration (db migrate)

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

- Stop qui before you run the migration.
- Create the target Postgres database before you run the migration.
- Set exactly one of `--dry-run` or `--apply`.
- The command copies all runtime tables except migration history.
- The migrator creates the schema and tables inside the destination database. It does not create the database itself.
- The output includes per-table row counts for SQLite and Postgres.
- On a new database, the dry run lists every table under `Missing Postgres tables` with `postgres=0`. This is expected. `--apply` creates them.
- `--apply` empties every table in the destination database before it copies rows. Point it only at a new or empty database. If qui already ran against that Postgres database, its data is lost. A non-zero `postgres=` count in the dry run output means the destination is not empty.

### FAQ

**Q: Why is `cross_seed_feed_items` row count lower in Postgres after migration?**

If the SQLite file contains historical rows whose `indexer_id` no longer exists in `torznab_indexers`, the migration skips them. Postgres enforces the foreign key, so the migration keeps only rows that have valid parent records.

You can check this in SQLite:

```sql
SELECT COUNT(*) AS orphaned_rows
FROM cross_seed_feed_items f
LEFT JOIN torznab_indexers i ON i.id = f.indexer_id
WHERE i.id IS NULL;
```

If `orphaned_rows` matches the migration delta (`sqlite_count - postgres_count`), the migration works as intended.
