#!/bin/sh
set -e

# Unraid maps appdata in from the host and expects containers to honour
# PUID/PGID, otherwise the database ends up owned by root and unreadable from
# the share. Create a matching user, take ownership of /data, then drop to it.
PUID=${PUID:-99}
PGID=${PGID:-100}

if [ "$(id -u)" = "0" ]; then
  if ! getent group geetproject >/dev/null 2>&1; then
    addgroup -g "$PGID" geetproject 2>/dev/null || true
  fi
  if ! getent passwd geetproject >/dev/null 2>&1; then
    adduser -D -H -u "$PUID" -G geetproject geetproject 2>/dev/null || true
  fi

  DB_DIR=$(dirname "${GEETPROJECT_DB:-/data/geetproject.db}")
  mkdir -p "$DB_DIR"
  # Only the data directory: chown -R over a bind mount with many files is slow
  # and there is nothing else here we should be touching.
  chown "$PUID:$PGID" "$DB_DIR"
  for f in "$DB_DIR"/*; do
    [ -e "$f" ] && chown "$PUID:$PGID" "$f"
  done

  exec su-exec "$PUID:$PGID" "$@"
fi

exec "$@"
