#!/bin/sh
set -e
if ! getent passwd homedash >/dev/null; then
  adduser --system --group --home /var/lib/homedash --no-create-home --shell /usr/sbin/nologin homedash
fi
install -d -m 0700 -o homedash -g homedash /var/lib/homedash
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload || true
  systemctl enable --now homedash.service || true
fi
if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database -q /usr/share/applications || true
fi
