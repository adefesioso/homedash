#!/bin/bash
# Build the .deb and install it on both hubs. Prints how to reach each panel.
. "$(dirname "$0")/lib.sh"
HUB=$(cd "$LAB_DIR/../hub" && pwd)

say "building the package"
( cd "$HUB" && make -s deb )
DEB=$(ls -t "$HUB"/dist/homedash_*_amd64.deb | head -1)

for row in "hub-a 10.1.0.10" "hub-b 10.2.0.10"; do
  set -- $row
  say "installing $(basename "$DEB") on $1 ($2)"
  scp "${SSH_OPTS[@]}" -o "ProxyCommand=$(jump)" "$DEB" "debian@$2:/tmp/homedash.deb" >/dev/null
  # A dirty tree builds the same version twice; the lab reinstalls anyway.
  guest "$2" "sudo DEBIAN_FRONTEND=noninteractive apt-get -qq install -y --allow-downgrades --reinstall /tmp/homedash.deb >/dev/null && sleep 2 && systemctl is-active homedash && curl -s localhost:7433/api/health"
  echo
done

cat <<TXT

Panels are on localhost inside each hub. To open one from this machine:
  ./ssh.sh hub-a -L 7433:localhost:7433
  ./ssh.sh hub-b -L 7434:localhost:7433
then http://localhost:7433 and http://localhost:7434.
TXT
