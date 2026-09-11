#!/bin/sh
set -e

PUID="${PUID:-1000}"
PGID="${PGID:-1000}"

# The claude CLI keeps its state under $HOME, which lives on the /config volume.
mkdir -p "$HOME" "$PROPOSARR_DATA_DIR"

if [ "$(id -u)" = "0" ]; then
  # A freshly created host directory is owned by root; hand it to PUID:PGID
  # (e.g. 99:100 on Unraid) before dropping privileges.
  chown "$PUID:$PGID" /config
  chown -R "$PUID:$PGID" "$HOME" "$PROPOSARR_DATA_DIR"
fi

case "$1" in
  serve|run|add|check|validate-token|version|help)
    # /usr/local/bin/proposarr drops root itself.
    exec proposarr "$@"
    ;;
esac
if [ "$(id -u)" = "0" ]; then
  exec setpriv --reuid="$PUID" --regid="$PGID" --clear-groups "$@"
fi
exec "$@"
