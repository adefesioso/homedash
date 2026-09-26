#!/bin/sh
# The fact-collection script. The heartbeat pipes this over stdin every
# minute; it is the only HomeDash code that ever runs on a remote, and it
# is never installed there. Prints one JSON object and exits.
set -u
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:$HOME/.local/bin
esc() { printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g' | tr -d '\n'; }
first=1
kv() { [ $first = 1 ] || printf ','; first=0; printf '"%s":%s' "$1" "$2"; }
str() { kv "$1" "\"$(esc "$2")\""; }

printf '{'
str hostname "$(hostname)"
str os "$(. /etc/os-release 2>/dev/null && printf '%s' "${PRETTY_NAME:-}")"
str kernel "$(uname -r)"
str arch "$(uname -m)"
kv cores "$(nproc)"
kv memTotal "$(awk '/MemTotal/{print $2*1024}' /proc/meminfo)"
kv memUsed "$(awk '/MemTotal/{t=$2} /MemAvailable/{a=$2} END{print (t-a)*1024}' /proc/meminfo)"
kv load1 "$(cut -d' ' -f1 /proc/loadavg)"
kv uptime "$(cut -d. -f1 /proc/uptime)"

# Mountpoints on real block devices: path, filesystem, size, free, source.
printf ',"mounts":['
df -B1 --output=target,fstype,size,avail,source -x tmpfs -x devtmpfs -x squashfs -x overlay -x efivarfs 2>/dev/null | tail -n +2 | awk '
  NR>1{printf ","} {printf "{\"path\":\"%s\",\"fs\":\"%s\",\"size\":%s,\"free\":%s,\"source\":\"%s\"}", $1,$2,$3,$4,$5}'
printf ']'

# Block devices: every disk and its partitions, with size, filesystem
# and mountpoint — lsblk already speaks JSON, so its tree goes straight
# in. A disk with no fstype of its own and no partition is raw: nothing
# to pool until it's formatted (S-1).
printf ',"disks":'
lsblk -J -b -o NAME,SIZE,FSTYPE,MOUNTPOINT,TYPE 2>/dev/null || printf '{"blockdevices":[]}'

# What the system, swap and the hub's own paths sit on: off-limits to clusters.
printf ',"systemSources":['
system_sources() { { for m in / /boot /boot/efi /var /home; do findmnt -no SOURCE "$m" 2>/dev/null; done; awk 'NR>1{print $1}' /proc/swaps; } | sed 's/\[.*\]$//' | sort -u; }
system_sources | awk 'NF{ if(n++) printf ","; printf "\"%s\"", $1 }'
printf ']'

# The same, walked down to the metal: each source and everything beneath
# it (LV, partition, whole disk) by kernel name. The gate refuses disk
# tools on exactly these and lets every other disk be formatted.
printf ',"systemDevices":['
{ system_sources; system_sources | while read -r s; do lsblk -lsno NAME "$s" 2>/dev/null | sed 's#^#/dev/#'; done; } | sort -u | awk 'NF{ if(n++) printf ","; printf "\"%s\"", $1 }'
printf ']'

# Interfaces with a hardware address: how another remote's scan recognises
# this one, and — with each IPv4 address and whether the kernel marks it
# dynamic (a lease) or not (held) — what the card's Address section shows.
printf ',"interfaces":['
ip -o link show 2>/dev/null | awk '$2!="lo:" && /link\/ether/ { for(i=1;i<=NF;i++) if($i=="link/ether") mac=$(i+1); n=$2; sub(":$","",n); sub("@.*","",n); print n, tolower(mac) }' | while read -r n mac; do
  [ "${c:-0}" = 0 ] || printf ','; c=1
  printf '{"name":"%s","mac":"%s","physical":%s,"addrs":[' "$n" "$mac" "$([ -e "/sys/class/net/$n/device" ] && echo true || echo false)"
  ip -o -4 addr show dev "$n" scope global 2>/dev/null | awk '{ dyn="false"; for(i=1;i<=NF;i++) if($i=="dynamic") dyn="true"; if(c++) printf ","; printf "{\"cidr\":\"%s\",\"dynamic\":%s}", $4, dyn }'
  printf ']}'
done
printf ']'

# The network as a whole: the default route, the resolver's servers, and
# which manager owns the config — the one address.sh would write to.
printf ',"network":{'
printf '"gateway":"%s","via":"%s"' "$(ip -4 route show default 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="via") print $(i+1); exit}')" "$(ip -4 route show default 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="dev") print $(i+1); exit}')"
printf ',"dns":['
{ resolvectl dns 2>/dev/null | awk -F': ' 'NF>1{print $2}' | tr ' ' '\n'; [ -f /etc/resolv.conf ] && awk '$1=="nameserver"{print $2}' /etc/resolv.conf; } | grep -E '^[0-9.]+$' | awk '!seen[$0]++ { if(c++) printf ","; printf "\"%s\"", $0 }'
printf ']'
if systemctl is-active -q NetworkManager 2>/dev/null; then nm=nm
elif command -v netplan >/dev/null 2>&1 && [ -d /etc/netplan ]; then nm=netplan
elif systemctl is-active -q systemd-networkd 2>/dev/null; then nm=networkd
elif command -v ifup >/dev/null 2>&1 && [ -f /etc/network/interfaces ]; then nm=ifupdown
else nm=""; fi
printf ',"manager":"%s"' "$nm"
printf ',"pending":%s' "$(systemctl is-active -q homedash-net-guard 2>/dev/null && echo true || echo false)"
printf '}'

# GPU: nvidia-smi if present; otherwise whether a DRM render node exists.
if command -v nvidia-smi >/dev/null 2>&1 && out=$(nvidia-smi --query-gpu=name,utilization.gpu,memory.total,memory.used --format=csv,noheader,nounits 2>/dev/null | head -1); then
  name=$(printf '%s' "$out" | cut -d, -f1 | sed 's/^ *//')
  util=$(printf '%s' "$out" | cut -d, -f2 | tr -d ' ')
  printf ',"gpu":{"name":"%s","busy":%s,"util":%s}' "$(esc "$name")" "$([ "${util:-0}" -gt 10 ] && echo true || echo false)" "${util:-0}"
elif ls /dev/dri/render* >/dev/null 2>&1; then
  printf ',"gpu":{"name":"%s","busy":false,"util":0}' "$(esc "$(lspci 2>/dev/null | grep -iE 'vga|3d' | head -1 | cut -d: -f3- | sed 's/^ *//')")"
else
  printf ',"gpu":null'
fi

# Docker and Ollama: installed, and running.
printf ',"docker":%s' "$(command -v docker >/dev/null 2>&1 && docker version --format '"{{.Server.Version}}"' 2>/dev/null || echo null)"
if command -v ollama >/dev/null 2>&1; then
  printf ',"ollama":{"installed":true,"running":%s}' "$(curl -fs -m 2 http://127.0.0.1:11434/api/version >/dev/null 2>&1 && echo true || echo false)"
else
  printf ',"ollama":{"installed":false,"running":false}'
fi

# The lock: what sshd would actually allow right now, for everyone.
if t=$(sudo -n sshd -T 2>/dev/null); then
  pa=$(printf '%s\n' "$t" | awk '$1=="passwordauthentication"{print $2}')
  pr=$(printf '%s\n' "$t" | awk '$1=="permitrootlogin"{print $2}')
  au=$(printf '%s\n' "$t" | awk '$1=="allowusers"{$1="";print}' | sed 's/^ //')
  locked=false
  [ "$pa" = no ] && [ "$pr" = no ] && [ "$au" = "$(whoami)" ] && locked=true
  printf ',"lock":{"locked":%s,"passwordAuth":"%s","permitRootLogin":"%s","allowUsers":"%s"}' "$locked" "$pa" "$pr" "$(esc "$au")"
else
  printf ',"lock":null'
fi

# The agent: version, configured model, snapshot age; whether its home's
# account exists; and whether the hub's hold is whole — its key, the sshd
# drop-in naming it, its account's sudoers line. The agent's state is
# readable only as root.
omp=$(command -v omp || ls /usr/local/bin/omp 2>/dev/null)
if [ -n "$omp" ]; then
  v=$("$omp" --version 2>/dev/null | sed 's#^omp/##')
  ah=/home/homedash-agent
  account=false; getent passwd homedash-agent >/dev/null 2>&1 && account=true
  hold=false
  sudo -n test -s /etc/ssh/authorized_keys.d/homedash && sudo -n test -f /etc/ssh/sshd_config.d/10-homedash-keys.conf &&
    sudo -n grep -qs '^homedash ALL=(ALL) NOPASSWD:ALL' /etc/sudoers.d/homedash && hold=true
  model=$(sudo -n awk '/^modelRoles:/{f=1;next} f&&/^  default:/{print $2;exit} /^[^ ]/{f=0}' "$ah/.omp/agent/config.yml" 2>/dev/null)
  if age=$(sudo -n stat -c %Y "$ah/.omp/cache/auth-broker-snapshot.enc" 2>/dev/null); then age=$(( $(date +%s) - age )); else age=null; fi
  printf ',"agent":{"version":"%s","model":"%s","snapshotAge":%s,"account":%s,"hold":%s}' "$(esc "$v")" "$(esc "$model")" "$age" "$account" "$hold"
else
  printf ',"agent":null'
fi
printf '}\n'
