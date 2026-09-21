#!/bin/sh
# Put the panel behind https://homedash.local on this machine, so
# passkeys work from any browser in the house rather than only on the
# hub's own localhost. Avahi answers to the name over mDNS, Caddy
# terminates TLS with its own local CA and proxies to the hub; the hub
# is told that origin is its passkey origin.
# See docs/running/https.md.
set -eu

CADDYFILE=/etc/caddy/Caddyfile
AVAHICONF=/etc/avahi/avahi-daemon.conf
CA=/var/lib/caddy/.local/share/caddy/pki/authorities/local/root.crt
UNIT=homedash.service

die() { echo "https: $*" >&2; exit 1; }

usage() {
  cat <<USAGE
usage: ${0##*/} [HOSTNAME]

Installs Caddy, serves the panel at https://HOSTNAME with a certificate
from Caddy's own local CA, points the hub's passkey origin at it and
restarts the hub. Run once, as root, on the hub. HOSTNAME defaults to
homedash.local; a name ending in .local is published over mDNS by
Avahi (this machine stops answering to $(hostname).local), any other
name has to resolve through your network's DNS.

Afterwards import the CA it leaves at ~/homedash-ca.crt into each
browser or device you open the panel from.
USAGE
}

case "${1:-}" in
  -h|--help) usage; exit 0 ;;
esac
[ "$(id -u)" = 0 ] || die "run as root"
command -v systemctl >/dev/null || die "needs systemd"
systemctl cat "$UNIT" >/dev/null 2>&1 || die "homedash is not installed here"

HOST="${1:-homedash.local}"
case "$HOST" in
  *:*|*/*) die "HOSTNAME is a bare name, not a URL" ;;
esac
# WebAuthn's RP ID must be a domain; the hub refuses an IP literal too.
if echo "$HOST" | grep -Eq '^[0-9.]+$|^[0-9a-fA-F:]+$'; then
  die "HOSTNAME must be a name, not an address ($HOST)"
fi

# Where the hub listens, from the unit; the default is the launcher's.
HUB=$(systemctl show "$UNIT" -p Environment | tr ' ' '\n' | sed -n 's/^HOMEDASH_ADDR=//p')
HUB="${HUB:-127.0.0.1:7433}"
case "$HUB" in
  0.0.0.0:*|:::*|\[::\]:*) HUB="127.0.0.1:${HUB##*:}" ;;
esac
curl -fsS "http://$HUB/api/health" >/dev/null || die "the hub does not answer at http://$HUB — is it running?"

echo "https: installing caddy"
if ! command -v caddy >/dev/null; then
  apt-get install -y caddy >/dev/null
fi

# A .local name is the hub's own mDNS name: Avahi publishes it, whatever
# the machine's hostname is, and moves with the address on its own.
case "$HOST" in
  *.local)
    NAME="${HOST%.local}"
    echo "https: answering to $HOST over mDNS"
    if ! command -v avahi-daemon >/dev/null; then
      apt-get install -y avahi-daemon libnss-mdns >/dev/null
    fi
    [ -f "$AVAHICONF.orig" ] || cp "$AVAHICONF" "$AVAHICONF.orig"
    sed -i -e '/^#\{0,1\}host-name=/d' -e "s/^\[server\]\$/[server]\nhost-name=$NAME/" "$AVAHICONF"
    grep -q "^host-name=$NAME\$" "$AVAHICONF" || die "could not set host-name in $AVAHICONF"
    systemctl enable --now avahi-daemon >/dev/null
    systemctl restart avahi-daemon
    ;;
esac

echo "https: serving https://$HOST -> http://$HUB"
[ -f "$CADDYFILE" ] && [ ! -f "$CADDYFILE.orig" ] && cp "$CADDYFILE" "$CADDYFILE.orig"
cat >"$CADDYFILE" <<CADDY
# Written by homedash's https.sh; the panel behind its own certificate.
$HOST {
	tls internal
	reverse_proxy $HUB
}
CADDY
systemctl enable --now caddy >/dev/null
systemctl reload caddy

echo "https: telling the hub its passkey origin is https://$HOST"
TOKEN=$(sudo -u homedash HOMEDASH_STATE=/var/lib/homedash homedash token https-setup)
curl -fsS -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"auth.rpid\":\"$HOST\",\"auth.origins\":\"https://$HOST\"}" "http://$HUB/api/settings" >/dev/null
curl -fsS -X DELETE -H "Authorization: Bearer $TOKEN" "http://$HUB/api/tokens/https-setup" >/dev/null
systemctl restart "$UNIT"
for _ in 1 2 3 4 5 6 7 8 9 10; do
  curl -fsS "http://$HUB/api/health" >/dev/null 2>&1 && break
  sleep 1
done
# The name may take a moment to resolve after Avahi comes up.
for _ in 1 2 3 4 5 6 7 8 9 10; do
  curl -fsSk "https://$HOST/api/health" >/dev/null 2>&1 && break
  sleep 1
done
curl -fsSk "https://$HOST/api/health" >/dev/null || die "https://$HOST does not answer; check 'journalctl -u caddy' and 'journalctl -u avahi-daemon'"

# The root CA the browser machines have to trust, where the person who
# ran this can scp it from.
OWNER="${SUDO_USER:-root}"
DEST=$(getent passwd "$OWNER" | cut -d: -f6)/homedash-ca.crt
cp "$CA" "$DEST"
chown "$OWNER" "$DEST"

cat <<DONE

Done. The panel is at https://$HOST — the launcher's http://localhost
no longer signs in, because a passkey is bound to one origin.

On each machine you browse from, trust Caddy's root CA once:
  scp $OWNER@$HOST:homedash-ca.crt .
then import homedash-ca.crt as a trusted authority (docs/running/https.md
says where, per system) and open https://$HOST.
DONE
