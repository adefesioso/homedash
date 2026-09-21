#!/bin/sh
set -e
# The state file is the part you can't rebuild: /var/lib/homedash stays,
# even on purge. Only the account and the unit go.
if [ "$1" = "purge" ]; then
  if getent passwd homedash >/dev/null; then
    deluser --system homedash >/dev/null 2>&1 || true
  fi
fi
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload || true
fi
