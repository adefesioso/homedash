#!/bin/bash
# Two houses inside Proxmox with only "the internet" between them.
#
# Each house is a bridge (vmbr1, vmbr2) whose gateway is a NAT router of its
# own: a network namespace with one veth leg on the house bridge and one on
# vmbr0 carrying that house's public address, masquerading like a home
# router. Proxmox holds a management address on each bridge and forwards
# nothing, so the only path from house A to house B is out through A's NAT,
# across vmbr0, and in through a mapping on B's NAT — a real hole punch.
#
# Owns a marked section of /etc/network/interfaces; re-running replaces it.
. "$(dirname "$0")/lib.sh"

section() {
cat <<CFG
# homedash-lab begin (managed by lab/pve-network.sh)
auto $HOUSE_A_BR
iface $HOUSE_A_BR inet static
	address $HOUSE_A_MGMT/24
	bridge-ports none
	bridge-stp off
	bridge-fd 0

auto $HOUSE_B_BR
iface $HOUSE_B_BR inet static
	address $HOUSE_B_MGMT/24
	bridge-ports none
	bridge-stp off
	bridge-fd 0
	post-up   /usr/local/sbin/homedash-lab-nat up
	post-down /usr/local/sbin/homedash-lab-nat down
# homedash-lab end
CFG
}

natscript() {
cat <<SH
#!/bin/sh
# Installed by lab/pve-network.sh. \$1 = up|down. One NAT router per house.
set -e
router() { # router <name> <bridge> <lan-gw/cidr> <public/cidr>
  ns=nat-\$1
  ip netns add \$ns
  ip link add \$ns-lan type veth peer name \$ns-lan-in
  ip link add \$ns-wan type veth peer name \$ns-wan-in
  ip link set \$ns-lan master \$2 up
  ip link set \$ns-wan master vmbr0 up
  ip link set \$ns-lan-in netns \$ns
  ip link set \$ns-wan-in netns \$ns
  ip netns exec \$ns sh -e <<NS
    ip link set lo up
    ip addr add \$3 dev \$ns-lan-in; ip link set \$ns-lan-in up
    ip addr add \$4 dev \$ns-wan-in; ip link set \$ns-wan-in up
    ip route add default via $UPSTREAM_GW
    sysctl -qw net.ipv4.ip_forward=1
    iptables -t nat -A POSTROUTING -o \$ns-wan-in -j MASQUERADE
    # Unsolicited inbound is dropped, and dropped before conntrack confirms
    # it — otherwise the stray entry steals the port a punch needs.
    iptables -A INPUT   -i \$ns-wan-in -m conntrack --ctstate NEW -j DROP
    iptables -A FORWARD -i \$ns-wan-in -m conntrack --ctstate NEW -j DROP
NS
}
case \$1 in
  up)
    router a $HOUSE_A_BR $HOUSE_A_GW/24 $HOUSE_A_PUBLIC/24
    router b $HOUSE_B_BR $HOUSE_B_GW/24 $HOUSE_B_PUBLIC/24
    # Proxmox is not a router between the houses.
    iptables -C FORWARD -s $HOUSE_A_NET -d $HOUSE_B_NET -j DROP 2>/dev/null || iptables -A FORWARD -s $HOUSE_A_NET -d $HOUSE_B_NET -j DROP
    iptables -C FORWARD -s $HOUSE_B_NET -d $HOUSE_A_NET -j DROP 2>/dev/null || iptables -A FORWARD -s $HOUSE_B_NET -d $HOUSE_A_NET -j DROP
    ;;
  down)
    ip netns del nat-a 2>/dev/null || true
    ip netns del nat-b 2>/dev/null || true
    ;;
  *) echo "usage: \$0 up|down"; exit 2;;
esac
SH
}

say "configuring house A ($HOUSE_A_NET behind $HOUSE_A_PUBLIC) and house B ($HOUSE_B_NET behind $HOUSE_B_PUBLIC)"
natscript | pve_in 'cat > /usr/local/sbin/homedash-lab-nat && chmod 755 /usr/local/sbin/homedash-lab-nat'
section | pve_in '
  set -e
  f=/etc/network/interfaces
  /usr/local/sbin/homedash-lab-nat down
  # leftovers from earlier versions of this script
  iptables -t nat -F POSTROUTING; iptables -F FORWARD
  ip addr del '"$HOUSE_B_PUBLIC"'/24 dev vmbr0 2>/dev/null || true
  ip addr del 10.20.30.148/24 dev vmbr0 2>/dev/null || true
  sed -i "/^# homedash-lab begin/,/^# homedash-lab end/d" $f
  sed -i "/^auto '"$HOUSE_A_BR"'$/,/^$/d; /^auto '"$HOUSE_B_BR"'$/,/^$/d" $f
  cat >> $f
  ifreload -a
  # ifreload can re-create a bridge under running guests; put their taps back.
  for t in /sys/class/net/tap*i0; do
    [ -e "$t" ] || continue; t=$(basename $t); id=${t#tap}; id=${id%i0}
    br=$(qm config $id 2>/dev/null | sed -n "s/^net0:.*bridge=\([a-z0-9]*\).*/\1/p")
    [ -n "$br" ] && ip link set $t master $br
  done
  echo "--- proxmox"; ip -br addr show '"$HOUSE_A_BR"'; ip -br addr show '"$HOUSE_B_BR"'
  echo "--- nat-a"; ip netns exec nat-a ip -br addr | grep -v "^lo"
  echo "--- nat-b"; ip netns exec nat-b ip -br addr | grep -v "^lo"
'
