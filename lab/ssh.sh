#!/bin/bash
# ssh.sh pve            shell on proxmox
# ssh.sh <name|ip> ...  shell (or command) on a guest by name or address
# ssh flags (-L/-R/-D/-N/-o) may appear before the remote command, e.g.
# ssh.sh hub-a -L 7433:localhost:7433 -N
. "$(dirname "$0")/lib.sh"
t=${1:-}; shift || true
[ -n "$t" ] || { echo "usage: ssh.sh pve|<guest> [-L/-R/-D/-N/-o ...] [cmd]"; echo "$VMS" | awk 'NF{print "  "$2"\t"$4}'; exit 2; }
sshflags=()
while true; do
  case "${1:-}" in
    -L|-R|-D|-o) sshflags+=("$1" "$2"); shift 2 ;;
    -N) sshflags+=("$1"); shift ;;
    *) break ;;
  esac
done
[ "$t" = pve ] && exec ssh "${SSH_OPTS[@]}" "${sshflags[@]}" "root@$(pve_ip)" "$@"
ip=$(echo "$VMS" | awk -v n="$t" 'NF && ($2==n || $4==n){print $4}')
[ -n "$ip" ] || die "no guest named $t"
exec ssh "${SSH_OPTS[@]}" "${sshflags[@]}" -o "ProxyCommand=$(jump)" "debian@$ip" "$@"
