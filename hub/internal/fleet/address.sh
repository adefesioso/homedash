#!/bin/sh
# Holding an address. The hub pipes this over stdin as root, with the
# interface, the mode (static|dhcp) and, for static, the address, the
# gateway and the DNS servers. It writes the config in whatever manages
# this machine's network, then hands over to a guard — a transient
# systemd unit — that applies it and waits for the hub to confirm from
# the new address. No confirmation in ninety seconds and the guard puts
# the saved config back. This script itself never changes the running
# network; the connection it came in on is still up when it exits.
set -eu
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
iface=$1; mode=$2; cidr=${3:-}; gw=${4:-}; dns=${5:-}
state=/var/lib/homedash/net
confirm=/run/homedash-net-confirm
unit=homedash-net-guard

ip link show "$iface" >/dev/null 2>&1 || { echo "no interface named $iface" >&2; exit 5; }
if systemctl is-active -q "$unit" 2>/dev/null; then
  echo "a previous address change is still waiting to be confirmed or reverted; try again in a minute" >&2; exit 6
fi
rm -rf "$state"; mkdir -p "$state/off"; chmod 0700 "$state"
rm -f "$confirm"

# Which manager owns the interface. NetworkManager only counts when it is
# up and has the interface; netplan (Ubuntu, Debian cloud images) sits
# above networkd or NM and is the file to write when it is installed.
if systemctl is-active -q NetworkManager 2>/dev/null && nmcli -g GENERAL.STATE device show "$iface" 2>/dev/null | grep -q connected; then m=nm
elif command -v netplan >/dev/null 2>&1 && [ -d /etc/netplan ]; then m=netplan
elif systemctl is-active -q systemd-networkd 2>/dev/null; then m=networkd
elif command -v ifup >/dev/null 2>&1 && [ -f /etc/network/interfaces ]; then m=ifupdown
else echo "no supported network manager: NetworkManager, netplan, systemd-networkd or ifupdown" >&2; exit 5; fi
echo "$m" > "$state/manager"

# save FILE: remember a file's current content, or its absence, for revert.
save() { if [ -e "$1" ]; then cp -p "$1" "$state/$(printf '%s' "$1" | tr / +).prev"; else : > "$state/$(printf '%s' "$1" | tr / +).absent"; fi; }
# put FILE MODE < content: write it, having saved it.
put() { save "$1"; cat > "$1.tmp"; chmod "$2" "$1.tmp"; mv -f "$1.tmp" "$1"; }
dnslist() { printf '%s' "$dns" | tr ' ' '\n' | grep . ; }

case $m in
nm)
  con=$(nmcli -g GENERAL.CONNECTION device show "$iface")
  [ -n "$con" ] || { echo "NetworkManager has no connection on $iface" >&2; exit 5; }
  printf '%s\n' "$con" > "$state/nm.con"
  # The four settings as they are, one per line (a blank line for an
  # unset one), for the revert. Not *.prev: that glob is the files.
  nmcli -g ipv4.method,ipv4.addresses,ipv4.gateway,ipv4.dns con show "$con" > "$state/nm.saved"
  echo "con=\$(cat $state/nm.con)" > "$state/apply.sh"
  if [ "$mode" = static ]; then
    printf 'nmcli con mod "$con" ipv4.method manual ipv4.addresses %s ipv4.gateway %s ipv4.dns %s\n' \
      "'$cidr'" "'$gw'" "'$(printf '%s' "$dns" | tr ' ' ',')'" >> "$state/apply.sh"
  else
    printf 'nmcli con mod "$con" ipv4.method auto ipv4.addresses "" ipv4.gateway "" ipv4.dns ""\n' >> "$state/apply.sh"
  fi
  # -w: a lease that never comes must not hold the guard past its own
  # window; the activation keeps trying in the background either way.
  echo 'nmcli -w 20 con up "$con"' >> "$state/apply.sh"
  ;;
netplan)
  # netplan merges address lists across files rather than overriding
  # them, so the interface's address keys come out of every other file
  # (each saved for revert); what else those files say about it stays.
  # netplan is Python, so python3 and yaml are there wherever it is.
  for y in /etc/netplan/*.yaml /etc/netplan/*.yml; do
    [ -f "$y" ] || continue
    case $y in */90-homedash-*) continue ;; esac
    if python3 - "$y" "$iface" <<'PY'
import sys, yaml
p, iface = sys.argv[1:3]
doc = yaml.safe_load(open(p)) or {}
eth = ((doc.get("network") or {}).get("ethernets") or {}).get(iface)
if not isinstance(eth, dict): sys.exit(0)
keys = [k for k in ("addresses", "dhcp4", "gateway4", "routes", "nameservers") if k in eth]
if not keys: sys.exit(0)
for k in keys: del eth[k]
open(p + ".tmp", "w").write("# Edited by HomeDash: this interface's address is set from the panel.\n" + yaml.safe_dump(doc, default_flow_style=False))
sys.exit(3)
PY
    then :; else
      rc=$?
      [ "$rc" = 3 ] || { echo "could not read $y" >&2; exit 5; }
      save "$y"; chmod --reference="$y" "$y.tmp"; mv -f "$y.tmp" "$y"
    fi
  done
  f=/etc/netplan/90-homedash-$iface.yaml
  {
    echo "# Written by HomeDash: this interface's address, set from the panel."
    echo "network:"; echo "  version: 2"; echo "  ethernets:"; echo "    $iface:"
    if [ "$mode" = static ]; then
      echo "      dhcp4: false"
      echo "      addresses: [$cidr]"
      [ -n "$gw" ] && { echo "      routes:"; echo "        - to: default"; echo "          via: $gw"; }
      [ -n "$dns" ] && { echo "      nameservers:"; echo "        addresses: [$(printf '%s' "$dns" | tr ' ' ',')]"; }
    else
      echo "      dhcp4: true"
    fi
  } | put "$f" 0600
  echo 'netplan apply' > "$state/apply.sh"
  ;;
networkd)
  f=/etc/systemd/network/00-homedash-$iface.network
  {
    echo "# Written by HomeDash: this interface's address, set from the panel."
    echo "[Match]"; echo "Name=$iface"; echo; echo "[Network]"
    if [ "$mode" = static ]; then
      echo "Address=$cidr"
      [ -n "$gw" ] && echo "Gateway=$gw"
      dnslist | sed 's/^/DNS=/'
    else
      echo "DHCP=ipv4"
    fi
  } | put "$f" 0644
  printf 'networkctl reload\nnetworkctl reconfigure %s\n' "$iface" > "$state/apply.sh"
  ;;
ifupdown)
  # ifupdown applies every stanza it finds for an interface, so the
  # others are commented out of /etc/network/interfaces and drop-ins that
  # name it are moved aside; the guard puts all of it back on revert.
  save /etc/network/interfaces
  awk -v i="$iface" '
    $1=="iface" && $2==i {skip=1; print "#" $0; next}
    ($1=="auto" || $1=="allow-hotplug") && $2==i {print "#" $0; next}
    $1=="iface" || $1=="auto" || $1 ~ /^allow-/ || $1=="mapping" || $1=="source" || $1=="source-directory" {skip=0}
    skip && NF {print "#" $0; next}
    {print}' "$state/+etc+network+interfaces.prev" > /etc/network/interfaces
  mkdir -p /etc/network/interfaces.d
  for d in /etc/network/interfaces.d/*; do
    [ -f "$d" ] || continue
    if grep -qE "^[[:space:]]*(iface|auto|allow-[a-z]+)[[:space:]]+$iface([[:space:]]|$)" "$d"; then
      mv "$d" "$state/off/"; printf '%s\n' "$d" >> "$state/off.list"
    fi
  done
  f=/etc/network/interfaces.d/homedash-$iface
  {
    echo "# Written by HomeDash: this interface's address, set from the panel."
    echo "auto $iface"
    if [ "$mode" = static ]; then
      echo "iface $iface inet static"; echo "    address $cidr"
      [ -n "$gw" ] && echo "    gateway $gw"
      [ -n "$dns" ] && echo "    dns-nameservers $dns"
    else
      echo "iface $iface inet dhcp"
    fi
  } | put "$f" 0644
  # ifdown with the new stanza only undoes that stanza's kind of config
  # (a DHCP stanza releases a lease and leaves a static address on the
  # interface), so the addresses are flushed in between: what the
  # machine then has is what the new stanza gave it, and nothing else.
  printf 'ifdown %s 2>/dev/null || true\nip -4 addr flush dev %s\nifup %s\n' "$iface" "$iface" "$iface" > "$state/apply.sh"
  ;;
esac

# cloud-init re-renders the network it was given on boot; once HomeDash
# holds the address, it stops. The file stays across modes.
if [ -d /etc/cloud ]; then
  mkdir -p /etc/cloud/cloud.cfg.d
  printf '# Written by HomeDash: the panel sets this machine'"'"'s address.\nnetwork: {config: disabled}\n' | put /etc/cloud/cloud.cfg.d/99-homedash-network.cfg 0644
fi

# The revert: every saved file back where it was, every moved file back,
# the previous NetworkManager settings restored, and the same apply.
{
  echo 'set +e'
  for p in "$state"/*.prev; do [ -e "$p" ] || continue; b=$(basename "$p" .prev | tr + /); echo "cp -p '$p' '$b'"; done
  for a in "$state"/*.absent; do [ -e "$a" ] || continue; b=$(basename "$a" .absent | tr + /); echo "rm -f '$b'"; done
  [ -e "$state/off.list" ] && while read -r d; do echo "mv '$state/off/$(basename "$d")' '$d'"; done < "$state/off.list"
  if [ "$m" = nm ]; then
    echo 'con=$(cat '"$state"'/nm.con)'
    echo 'prev='"$state"'/nm.saved'
    echo 'nmcli con mod "$con" ipv4.method "$(sed -n 1p $prev)" ipv4.addresses "$(sed -n 2p $prev)" ipv4.gateway "$(sed -n 3p $prev)" ipv4.dns "$(sed -n 4p $prev)"'
    echo 'nmcli -w 20 con up "$con"'
  else
    cat "$state/apply.sh"
  fi
} > "$state/revert.sh"

cat > "$state/guard.sh" <<EOF
#!/bin/sh
sh $state/apply.sh
: > $state/applied
i=0
while [ \$i -lt 90 ]; do
  if [ -e $confirm ]; then rm -f $confirm; echo confirmed > $state/result; exit 0; fi
  sleep 2; i=\$((i+2))
done
sh $state/revert.sh
echo reverted > $state/result
EOF
chmod 0700 "$state/guard.sh"
if command -v systemd-run >/dev/null 2>&1; then
  systemd-run --quiet --unit "$unit" --collect sh "$state/guard.sh"
else
  setsid nohup sh "$state/guard.sh" >/dev/null 2>&1 < /dev/null &
fi
echo "$m"
