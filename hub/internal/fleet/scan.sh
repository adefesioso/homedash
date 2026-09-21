#!/bin/sh
# The scan script. The scan loop pipes this over stdin on its interval;
# like facts.sh it is never installed. It reads what the machine can
# already see or hear — neighbours, mDNS and SSDP announcements, a Wi-Fi
# scan, a few seconds of Bluetooth discovery — and prints one JSON object.
# It sends ICMP echo and multicast queries and connects to nothing.
set -u
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
raw=$(mktemp) || exit 1
trap 'rm -f "$raw"' EXIT
has() { command -v "$1" >/dev/null 2>&1; }

# --- LAN ------------------------------------------------------------
# The machine's own virtual networks — containers, VMs, tunnels — are
# not the house; nothing on them is a device.
virtual='^(docker|br-|veth|virbr|vnet|lxc|lxd|cni|flannel|tailscale|wg|tun|zt)'

# Populate the neighbour table: one echo per address on each /24 or
# smaller subnet this machine sits on, then the announcements.
ip -o -4 addr show scope global 2>/dev/null | awk -v v="$virtual" '$2 !~ v {print $4}' | while read -r cidr; do
  pfx=${cidr#*/}; [ "$pfx" -ge 24 ] || continue
  if has fping; then
    fping -a -q -g -r0 -t200 "$cidr" >/dev/null 2>&1
  else
    base=$(printf '%s' "${cidr%/*}" | cut -d. -f1-3)
    seq 1 254 | xargs -P 64 -I{} sh -c "ping -c1 -W1 $base.{} >/dev/null 2>&1" 2>/dev/null
  fi
done

if has avahi-browse; then
  # =;iface;proto;name;type;domain;host;address;port;txt
  timeout 8 avahi-browse -atrp 2>/dev/null | awk -F';' '$1=="=" && $3=="IPv4" {
    printf "H %s %s\n", $8, $7; printf "S %s %s\n", $8, $5; printf "M %s %s\n", $8, $4 }' >>"$raw"
fi

if has python3; then
  # One query per address this machine holds, so a multi-homed box asks on
  # every LAN it sits on rather than whichever the default route picks.
  ip -o -4 addr show scope global 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | timeout 8 python3 -c '
import select, socket, sys, time
m = b"M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 2\r\nST: ssdp:all\r\n\r\n"
socks = []
for a in sys.stdin.read().split():
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM, socket.IPPROTO_UDP)
    try:
        s.setsockopt(socket.IPPROTO_IP, socket.IP_MULTICAST_IF, socket.inet_aton(a)); s.bind((a, 0))
    except OSError:
        continue
    socks.append(s)
    for _ in range(2): s.sendto(m, ("239.255.255.250", 1900))
end = time.time() + 3
while socks and time.time() < end:
    for s in select.select(socks, [], [], 0.2)[0]:
        data, (ip, _) = s.recvfrom(4096)
        h = {}
        for line in data.decode(errors="replace").split("\r\n")[1:]:
            if ":" in line:
                k, v = line.split(":", 1); h[k.strip().lower()] = v.strip()
        print("U %s %s\t%s" % (ip, h.get("st", "").replace(" ", "_"), h.get("server", "").replace("\t", " ")))
' 2>/dev/null >>"$raw"
fi

ip -4 neigh show 2>/dev/null | awk -v v="$virtual" '$0 !~ /FAILED|INCOMPLETE/ && /lladdr/ {
  for(i=1;i<=NF;i++){ if($i=="dev") d=$(i+1); if($i=="lladdr") m=$(i+1) }
  if(d !~ v) printf "N %s %s %s\n", $1, d, tolower(m) }' >>"$raw"

# Hostnames the router or a local dnsmasq hands out.
awk '$1=="N"{print $2}' "$raw" | sort -u | while read -r ip; do
  n=$(getent hosts "$ip" 2>/dev/null | awk '{print $2}')
  [ -z "$n" ] && [ -r /var/lib/misc/dnsmasq.leases ] && n=$(awk -v ip="$ip" '$3==ip && $4!="*"{print $4}' /var/lib/misc/dnsmasq.leases)
  [ -n "$n" ] && printf 'H %s %s\n' "$ip" "$n" >>"$raw"
done

# --- Wi-Fi ------------------------------------------------------------
wifi=null
wl=$(iw dev 2>/dev/null | awk '$1=="Interface"{print $2; exit}')
if [ -n "$wl" ]; then
  wifi=list
  # Signal is a percentage either way: nmcli reports one, iw reports dBm.
  if has nmcli && timeout 20 nmcli -t -f BSSID,SSID,SIGNAL,FREQ,SECURITY dev wifi list --rescan yes >"$raw.w" 2>/dev/null; then
    sed 's/\\:/-/g' "$raw.w" | awk -F: 'NF>=4 { gsub("-",":",$1); printf "W %s\t%s\t%s\t%s\t%s\n", tolower($1), $2, $3, $4+0, $5 }' >>"$raw"
  else
    timeout 20 sudo -n iw dev "$wl" scan 2>/dev/null | awk '
      function pct(d){ d=2*(d+100); return d<0?0:(d>100?100:int(d)) }
      /^BSS /{ if(b!="") printf "W %s\t%s\t%s\t%s\t%s\n", b, s, sig, f, sec; b=tolower(substr($2,1,17)); s=""; sig=""; f=""; sec="" }
      /^\tSSID:/{ s=substr($0,8) } /^\tsignal:/{ sig=pct($2) } /^\tfreq:/{ f=$2 }
      /^\tRSN:/{ sec="WPA2" } /^\tWPA:/{ if(sec=="") sec="WPA" }
      END{ if(b!="") printf "W %s\t%s\t%s\t%s\t%s\n", b, s, sig, f, sec }' >>"$raw"
  fi
  rm -f "$raw.w"
fi

# --- Bluetooth --------------------------------------------------------
bt=null
if has bluetoothctl && timeout 3 bluetoothctl list 2>/dev/null | grep -q Controller; then
  bt=list
  timeout 10 bluetoothctl --timeout 8 scan on >/dev/null 2>&1
  timeout 3 bluetoothctl devices 2>/dev/null | awk '$1=="Device"{ m=tolower($2); $1=""; $2=""; sub(/^  /,""); print m "\t" $0 }' | head -40 | while IFS="$(printf '\t')" read -r mac name; do
    info=$(timeout 2 bluetoothctl info "$mac" 2>/dev/null)
    icon=$(printf '%s\n' "$info" | awk '$1=="Icon:"{print $2}')
    rssi=$(printf '%s\n' "$info" | awk '$1=="RSSI:"{print $2}')
    printf 'B %s\t%s\t%s\t%s\n' "$mac" "$name" "$icon" "${rssi:-}" >>"$raw"
  done
fi

# --- Emit ---------------------------------------------------------------
awk -v wifi="$wifi" -v bt="$bt" '
function esc(s){ gsub(/\\/,"\\\\",s); gsub(/"/,"\\\"",s); gsub(/[\001-\037]/,"",s); return s }
function num(s){ return (s ~ /^-?[0-9.]+$/) ? s : "null" }
$1=="N"{ ip=$2; if(!(ip in mac)){ order[++n]=ip } mac[ip]=$4; dev[ip]=$3 }
$1=="H"{ if(host[$2]=="") host[$2]=$3 }
$1=="M"{ if(mname[$2]=="") { s=$0; sub(/^M [^ ]+ /,"",s); mname[$2]=s } }
$1=="S"{ if(index(svc[$2], $3 ",")==0) svc[$2]=svc[$2] $3 "," }
$1=="U"{ s=$0; sub(/^U [^ ]+ /,"",s); split(s,p,"\t"); if(index(sst[$2],p[1] ",")==0) sst[$2]=sst[$2] p[1] ","; if(sserver[$2]=="") sserver[$2]=p[2] }
$1=="W"{ s=$0; sub(/^W /,"",s); wl[++wn]=s }
$1=="B"{ s=$0; sub(/^B /,"",s); bl[++bn]=s }
END{
  printf "{\"lan\":["
  for(i=1;i<=n;i++){ ip=order[i]
    if(i>1) printf ","
    printf "{\"mac\":\"%s\",\"ip\":\"%s\",\"iface\":\"%s\",\"hostname\":\"%s\",\"mdnsName\":\"%s\",\"ssdpServer\":\"%s\",\"services\":[", mac[ip], ip, esc(dev[ip]), esc(host[ip]), esc(mname[ip]), esc(sserver[ip])
    k=split(svc[ip], a, ","); c=0
    for(j=1;j<=k;j++) if(a[j]!=""){ if(c++) printf ","; printf "\"%s\"", esc(a[j]) }
    printf "],\"ssdp\":["
    k=split(sst[ip], a, ","); c=0
    for(j=1;j<=k;j++) if(a[j]!=""){ if(c++) printf ","; printf "\"%s\"", esc(a[j]) }
    printf "]}"
  }
  printf "],\"wifi\":"
  if(wifi=="null") printf "null"; else {
    printf "["
    for(i=1;i<=wn;i++){ split(wl[i],p,"\t"); if(i>1) printf ","
      printf "{\"bssid\":\"%s\",\"ssid\":\"%s\",\"signal\":%s,\"freq\":%s,\"security\":\"%s\"}", esc(p[1]), esc(p[2]), num(p[3]), num(p[4]), esc(p[5]) }
    printf "]"
  }
  printf ",\"bt\":"
  if(bt=="null") printf "null"; else {
    printf "["
    for(i=1;i<=bn;i++){ split(bl[i],p,"\t"); if(i>1) printf ","
      printf "{\"mac\":\"%s\",\"name\":\"%s\",\"icon\":\"%s\",\"rssi\":%s}", esc(p[1]), esc(p[2]), esc(p[3]), num(p[4]) }
    printf "]"
  }
  printf "}\n"
}' "$raw"
