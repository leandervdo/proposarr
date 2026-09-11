#!/bin/sh
# Installed as /usr/local/bin/proposarr. `docker exec` runs as root by default;
# drop to PUID:PGID so commands never leave root-owned files in /config.
if [ "$(id -u)" = "0" ]; then
  exec setpriv --reuid="${PUID:-1000}" --regid="${PGID:-1000}" --clear-groups /usr/local/bin/proposarr-bin "$@"
fi
exec /usr/local/bin/proposarr-bin "$@"
