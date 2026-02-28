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

## Database Migration

Offline SQLite to Postgres migration:

```bash
# 0) Stop qui first (no writes during migration)
#    (example) docker compose stop qui

# 1) Optional: backup the SQLite file
cp /path/to/qui.db /path/to/qui.db.bak

# 2) Validate source + destination without importing rows
./qui db migrate \
  --from-sqlite /path/to/qui.db \
  --to-postgres "postgres://user:pass@localhost:5432/qui?sslmode=disable" \
  --dry-run

# 3) Apply migration (schema bootstrap + table copy + identity reset)
./qui db migrate \
  --from-sqlite /path/to/qui.db \
  --to-postgres "postgres://user:pass@localhost:5432/qui?sslmode=disable" \
  --apply

# 4) Point qui at Postgres and start it again
#    - config.toml: databaseEngine=postgres + databaseDsn=...
#    - or env: QUI__DATABASE_ENGINE=postgres + QUI__DATABASE_DSN=...
```

Notes:

- Run this while qui is stopped.
- `--dry-run` and `--apply` are mutually exclusive.
- The command copies all runtime tables except migration history.
- The output includes per-table row counts for SQLite and Postgres.
