#!/bin/sh
set -e
if command -v systemctl >/dev/null 2>&1; then
  systemctl disable --now homedash.service || true
fi
# The hub's copy of omp is fetched, not state; the rest of /var/lib/homedash
# stays because it is the part you can't rebuild.
rm -rf /var/lib/homedash/omp/bin
