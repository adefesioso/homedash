# Shared helpers. Every script does `. "$(dirname "$0")/lib.sh"`.
set -euo pipefail
LAB_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
. "$LAB_DIR/lab.env"

say() { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

SSH_OPTS=(-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o ConnectTimeout=5 -i "$LAB_KEY")

# pve_ip: the Proxmox VM's address, resolved from its own MAC. Proxmox sets
# a static address, so the libvirt lease table is not the source of truth —
# and guests inside Proxmox on vmbr0 show up there too.
pve_ip() {
  local ip
  ip=$(virsh -q domifaddr "$PVE_VM" --source arp 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | head -1)
  if [ -n "$ip" ]; then echo "$ip" > "$PVE_DIR/pve-ip"; echo "$ip"; return; fi
  # The ARP entry ages out between calls; the last answer is still right.
  cat "$PVE_DIR/pve-ip" 2>/dev/null
}

# pve: run a command on the Proxmox host as root. `-n` keeps ssh off stdin
# so it can run inside a `while read` loop; pipe into pve_in instead.
pve()    { ssh -n "${SSH_OPTS[@]}" "root@$(pve_ip)" "$@"; }
pve_in() { ssh    "${SSH_OPTS[@]}" "root@$(pve_ip)" "$@"; }

# jump: the ProxyCommand that reaches a guest through Proxmox. (-J would not
# forward the -i identity to the jump hop.)
jump() { echo "ssh ${SSH_OPTS[*]} -W %h:%p root@$(pve_ip)"; }

# guest: run a command on a lab guest.
guest() { # guest <ip> <cmd...>
  local ip=$1; shift
  ssh -n "${SSH_OPTS[@]}" -o "ProxyCommand=$(jump)" "debian@$ip" "$@"
}

wait_for_ssh() { # wait_for_ssh <label> <fn> [timeout-s]
  local label=$1 fn=$2 t=${3:-300} i=0
  say "waiting for $label to answer on SSH"
  until "$fn" true 2>/dev/null; do
    sleep 5; i=$((i+5)); [ "$i" -ge "$t" ] && die "$label did not come up within ${t}s"
  done
}

# each_vm <fn>: calls fn id name bridge ip cores mem disks for each VM row.
each_vm() {
  echo "$VMS" | awk 'NF' | while read -r id name br ip cores mem disks; do
    "$1" "$id" "$name" "$br" "$ip" "$cores" "$mem" "$disks"
  done
}
