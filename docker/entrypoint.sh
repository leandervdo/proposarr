#!/bin/sh
set -e

# The claude CLI keeps its state under $HOME, which lives on the /config volume.
mkdir -p "$HOME" "$PROPOSARR_DATA_DIR"

case "$1" in
  run|add|check|validate-token|version|help)
    exec proposarr "$@"
    ;;
esac
exec "$@"
