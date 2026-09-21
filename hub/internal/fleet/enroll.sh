#!/bin/sh
# Enrollment. Fetched with the single-use code and run as root on the new
# machine: `curl -fsSL http://HUB/enroll/CODE | sudo sh`. Lays the machine
# out (the two accounts, the hub's key, Docker, the agent, the cage) and
# reports its specs and host key back. Everything that needs a secret (the
# vault token, the credential snapshot) happens afterwards, over the SSH
# connection the hub opens with the key this script authorized.
set -eu
HUB_URL='{{.HubURL}}'
CODE='{{.Code}}'
NAME='{{.Name}}'
[ "$(id -u)" = 0 ] || { echo "run this as root: curl -fsSL $HUB_URL/enroll/$CODE | sudo sh" >&2; exit 1; }

{{.Layout}}

say "reporting back"
HOSTKEY=$(cat /etc/ssh/ssh_host_ed25519_key.pub)
PORT=$( (sshd -T 2>/dev/null || true) | awk '$1=="port"{print $2; exit}'); PORT=${PORT:-22}
FACTS=$(su -s /bin/sh "$ACCOUNT" -c 'sh -s' <<'FACTS'
{{.Facts}}
FACTS
)
BODY=$(printf '{"name":"%s","port":%s,"hostKey":"%s","facts":%s}' "$NAME" "$PORT" "$HOSTKEY" "$FACTS")
printf '%s' "$BODY" | curl -fsS -X POST -H 'Content-Type: application/json' --data-binary @- "$HUB_URL/enroll/$CODE" >/dev/null
say "enrolled as $NAME; the hub takes it from here"
