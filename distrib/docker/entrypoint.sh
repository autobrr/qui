#!/bin/sh
set -e

# UMASK is handled by the qui binary itself (applyUmask in cmd/qui),
# so the entrypoint does not touch it.

# Docker's user setting has already dropped privileges, so run qui directly.
if [ "$(id -u)" -ne 0 ]; then
    exec /usr/local/bin/qui "$@"
fi

# Fail fast if only one of PUID/PGID is set
if { [ -n "$PUID" ] && [ -z "$PGID" ]; } || { [ -z "$PUID" ] && [ -n "$PGID" ]; }; then
    echo >&2 "ERROR: PUID and PGID must be set together"
    exit 1
fi

# Validate PUID/PGID are numeric
if [ -n "$PUID" ]; then
    case "$PUID" in *[!0-9]*|"") echo >&2 "ERROR: PUID must be a numeric uid"; exit 1;; esac
    case "$PGID" in *[!0-9]*|"") echo >&2 "ERROR: PGID must be a numeric gid"; exit 1;; esac
fi

# If PUID/PGID are set, run as that user
if [ -n "$PUID" ] && [ -n "$PGID" ]; then
    # Create group if GID doesn't exist in /etc/group
    if ! grep -q "^[^:]*:[^:]*:${PGID}:" /etc/group; then
        addgroup -g "$PGID" qui
    fi

    # Get group name for this GID
    GROUP_NAME=$(awk -F: -v gid="$PGID" '$3 == gid { print $1 }' /etc/group)

    # Create user if UID doesn't exist in /etc/passwd
    if ! grep -q "^[^:]*:[^:]*:${PUID}:" /etc/passwd; then
        adduser -D -H -u "$PUID" -G "$GROUP_NAME" -s /sbin/nologin qui
    fi

    # Fix ownership of anything in /config not owned by PUID:PGID.
    # A full-tree scan, not just the root dir: a correctly-owned mount can
    # still hold root-owned files from a run before PUID/PGID were set.
    mkdir -p /config
    # chown -h so symlinks are not followed (a link out of /config must not
    # chown its target). A file that cannot be chowned (read-only mount) is
    # not fatal: warn and let qui decide if it can live with it.
    find /config \( ! -user "$PUID" -o ! -group "$PGID" \) -exec chown -h "$PUID:$PGID" {} + \
        || echo >&2 "WARN: could not fix ownership of some files under /config"

    # Drop privileges and exec qui
    exec su-exec "$PUID:$PGID" /usr/local/bin/qui "$@"
fi

# No PUID/PGID set, run as current user (root in container)
exec /usr/local/bin/qui "$@"
