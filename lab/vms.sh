#!/bin/bash
# The guests: one Debian cloud-image template, cloned per row of $VMS with
# cloud-init for its address and the lab key. Idempotent: existing VMs are
# left alone.
. "$(dirname "$0")/lib.sh"

if ! pve "qm status $TEMPLATE_ID" >/dev/null 2>&1; then
  say "building template $TEMPLATE_ID from $(basename "$DEBIAN_IMG_URL")"
  pve "
    set -e
    cd /var/lib/vz/template
    [ -f debian-13.qcow2 ] || curl -sSL -o debian-13.qcow2 '$DEBIAN_IMG_URL'
    qm create $TEMPLATE_ID --name debian-13-template --memory 1024 --cores 1 --cpu host \
      --net0 virtio,bridge=vmbr0 --scsihw virtio-scsi-single --agent enabled=1 --ostype l26 --serial0 socket --vga serial0
    qm importdisk $TEMPLATE_ID debian-13.qcow2 $PVE_STORAGE >/dev/null
    qm set $TEMPLATE_ID --scsi0 $PVE_STORAGE:vm-$TEMPLATE_ID-disk-0,discard=on --boot order=scsi0 \
      --ide2 $PVE_STORAGE:cloudinit --ciuser debian --sshkeys /root/.ssh/authorized_keys --ciupgrade 0
    qm resize $TEMPLATE_ID scsi0 8G
    qm template $TEMPLATE_ID
  "
fi

create_vm() { # id name bridge ip cores mem disks
  local id=$1 name=$2 br=$3 ip=$4 cores=$5 mem=$6 disks=$7 gw
  # An existing guest is left as it is, except that a stopped one (Proxmox
  # was rebooted; guests do not autostart) is started again.
  if pve "qm status $id" >/dev/null 2>&1; then
    if pve "qm status $id" | grep -q stopped; then say "$name ($id) exists, starting"; pve "qm start $id"; else say "$name ($id) exists"; fi
    return
  fi
  case $br in "$HOUSE_A_BR") gw=$HOUSE_A_GW;; "$HOUSE_B_BR") gw=$HOUSE_B_GW;; *) die "unknown bridge $br";; esac
  say "creating $name ($id): $cores cores, ${mem}MB, $br $ip, extra disks: $disks"
  local extra="" n=1
  if [ "$disks" != "-" ]; then
    for g in ${disks//,/ }; do extra="$extra --scsi$n $PVE_STORAGE:$g"; n=$((n+1)); done
  fi
  pve "
    set -e
    qm clone $TEMPLATE_ID $id --name $name --full >/dev/null
    qm set $id --cores $cores --memory $mem --net0 virtio,bridge=$br \
      --ipconfig0 ip=$ip/24,gw=$gw --nameserver 10.20.30.1 --searchdomain homedash.lab $extra >/dev/null
    qm start $id
  "
}
each_vm create_vm

wait_guest() { # id name bridge ip ...
  local name=$2 ip=$4 i=0
  say "waiting for $name ($ip)"
  until guest "$ip" true 2>/dev/null; do sleep 5; i=$((i+5)); [ $i -ge 300 ] && die "$name did not come up"; done
  guest "$ip" "sudo apt-get -qq update && sudo DEBIAN_FRONTEND=noninteractive apt-get -qq install -y qemu-guest-agent >/dev/null && sudo systemctl start qemu-guest-agent"
}
each_vm wait_guest
say "all guests up"; pve "qm list"
