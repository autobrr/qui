#!/bin/sh
set -e

# Apply UMASK if set
if [ -n "$UMASK" ]; then
    umask "$UMASK"
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

    # Fix ownership of /config (skip if already correct)
    mkdir -p /config
    current_uid=$(stat -c %u /config 2>/dev/null || echo "")
    current_gid=$(stat -c %g /config 2>/dev/null || echo "")
    if [ -z "$current_uid" ] || [ -z "$current_gid" ] || \
       [ "$current_uid" -ne "$PUID" ] || [ "$current_gid" -ne "$PGID" ]; then
        chown -R "$PUID:$PGID" /config
    fi

    # Drop privileges and exec qui
    exec su-exec "$PUID:$PGID" /usr/local/bin/qui "$@"
fi

# No PUID/PGID set, run as current user (root in container)
exec /usr/local/bin/qui "$@"
