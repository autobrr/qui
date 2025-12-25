#!/bin/sh
set -e

# Apply UMASK if set
if [ -n "$UMASK" ]; then
    umask "$UMASK"
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

    # Fix ownership of /config
    chown -R "$PUID:$PGID" /config

    # Drop privileges and exec qui
    exec su-exec "$PUID:$PGID" /usr/local/bin/qui "$@"
fi

# No PUID/PGID set, run as current user (root in container)
exec /usr/local/bin/qui "$@"
