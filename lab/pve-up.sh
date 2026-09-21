#!/bin/bash
# Bring the Proxmox VM up under libvirt: define it if absent (unattended
# install from the Proxmox ISO), size it, start it, wait for SSH.
. "$(dirname "$0")/lib.sh"

[ -f "$LAB_KEY" ] || { mkdir -p "$(dirname "$LAB_KEY")"; ssh-keygen -t ed25519 -N '' -C homedash-lab -f "$LAB_KEY" >/dev/null; }

if ! virsh -q net-info "$PVE_NET" >/dev/null 2>&1; then
  say "defining libvirt network $PVE_NET"
  cat > "$PVE_DIR/net.xml" <<XML
<network><name>$PVE_NET</name><forward mode='nat'/><bridge name='virbr-hpve' stp='on' delay='0'/>
<ip address='10.20.30.1' netmask='255.255.255.0'><dhcp><range start='10.20.30.10' end='10.20.30.250'/></dhcp></ip></network>
XML
  virsh net-define "$PVE_DIR/net.xml" && virsh net-autostart "$PVE_NET"
fi
virsh -q net-list --name | grep -qx "$PVE_NET" || virsh net-start "$PVE_NET"

if ! virsh -q dominfo "$PVE_VM" >/dev/null 2>&1; then
  [ -f "$PVE_ISO" ] || die "Proxmox ISO not found at $PVE_ISO — download it from https://www.proxmox.com/en/downloads"
  say "building unattended-install ISO"
  ROOTPW=$(cat "$PVE_DIR/root-password" 2>/dev/null || { tr -dc a-z0-9 </dev/urandom | head -c 12 | tee "$PVE_DIR/root-password"; })
  cat > "$PVE_DIR/answer.toml" <<TOML
[global]
keyboard = "en-us"
country = "us"
fqdn = "pve.homedash.lab"
mailto = "root@homedash.lab"
timezone = "UTC"
root-password = "$ROOTPW"
root-ssh-keys = ["$(cat "$LAB_KEY.pub")"]
reboot-on-error = true

[network]
source = "from-dhcp"

[disk-setup]
filesystem = "ext4"
disk-list = ["vda"]
TOML
  command -v proxmox-auto-install-assistant >/dev/null || die "need proxmox-auto-install-assistant (apt install proxmox-auto-install-assistant from the Proxmox repo)"
  proxmox-auto-install-assistant prepare-iso "$PVE_ISO" --fetch-from iso --answer-file "$PVE_DIR/answer.toml" --output "$PVE_DIR/autoinstall.iso"
  qemu-img create -f qcow2 "$PVE_DIR/$PVE_VM.qcow2" "$PVE_DISK" >/dev/null
  say "defining $PVE_VM (install takes ~5 min; it reboots itself when done)"
  virt-install --name "$PVE_VM" --memory "$(numfmt --from=iec "${PVE_MEM%G}G" | awk '{print $1/1048576}')" --vcpus "$PVE_VCPUS" \
    --cpu host-passthrough --os-variant debian12 \
    --disk "$PVE_DIR/$PVE_VM.qcow2,bus=virtio" --cdrom "$PVE_DIR/autoinstall.iso" \
    --network "network=$PVE_NET,model=virtio" --graphics none --noautoconsole --wait=-1 >/dev/null || true
fi

if [ "$(virsh -q domstate "$PVE_VM")" != "running" ]; then
  say "sizing $PVE_VM to $PVE_VCPUS vCPU / $PVE_MEM"
  virsh -q setvcpus "$PVE_VM" "$PVE_VCPUS" --config --maximum
  virsh -q setvcpus "$PVE_VM" "$PVE_VCPUS" --config
  virsh -q setmaxmem "$PVE_VM" "$PVE_MEM" --config
  virsh -q setmem "$PVE_VM" "$PVE_MEM" --config
  say "starting $PVE_VM"
  virsh -q start "$PVE_VM"
fi

until [ -n "$(pve_ip)" ]; do sleep 3; done
say "waiting for proxmox ($(pve_ip)) on port 22"
i=0; until nc -z -w2 "$(pve_ip)" 22 2>/dev/null; do sleep 5; i=$((i+5)); [ $i -ge 300 ] && die "proxmox did not come up"; done

# An install made before the key was in the answer file: put it there now.
if ! pve true 2>/dev/null; then
  [ -f "$PVE_DIR/root-password" ] || die "lab key is not authorized on proxmox and no $PVE_DIR/root-password to fall back on"
  say "authorizing the lab key with the root password"
  SSHPASS=$(cat "$PVE_DIR/root-password") sshpass -e ssh-copy-id -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$LAB_KEY.pub" "root@$(pve_ip)" >/dev/null 2>&1
  pve true || die "still cannot log in to proxmox with the lab key"
fi

# Nested virtualization and the lab key must both hold, or nothing below works.
pve "grep -qE 'vmx|svm' /proc/cpuinfo" || die "no nested virtualization inside the Proxmox VM; check /sys/module/kvm_amd/parameters/nested on the host"
say "proxmox is up at $(pve_ip)"
