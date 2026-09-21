#!/bin/bash
# Proxmox serves this lab and nothing else: destroy every VM that is not in
# the VM table or the template, and drop stray disks and ISOs. Asks first
# unless given --yes.
. "$(dirname "$0")/lib.sh"

keep=" $TEMPLATE_ID $(echo "$VMS" | awk 'NF{printf "%s ", $1}')"
stray=$(pve "qm list | awk 'NR>1{print \$1, \$2}'" | while read -r id name; do
  case "$keep" in *" $id "*) ;; *) echo "$id $name";; esac
done)

if [ -z "$stray" ]; then say "nothing on proxmox that isn't the lab"; exit 0; fi
echo "Will destroy on proxmox, with their disks:"; echo "$stray" | sed 's/^/  /'
if [ "${1:-}" != "--yes" ]; then read -r -p "Proceed? [y/N] " a; [ "$a" = y ] || exit 1; fi

echo "$stray" | while read -r id name; do
  pve "qm stop $id >/dev/null 2>&1 || true; qm destroy $id --purge --destroy-unreferenced-disks 1 >/dev/null" && say "destroyed $id $name"
done
# Disks no VM references any more, and downloaded images the template no longer needs.
pve '
  for lv in $(lvs --noheadings -o lv_name pve | grep -E "^ *(vm|base)-[0-9]+-" ); do
    id=$(echo $lv | sed -E "s/^(vm|base)-([0-9]+)-.*/\2/")
    qm status $id >/dev/null 2>&1 || { lvremove -fy pve/$lv >/dev/null && echo "removed orphan disk $lv"; }
  done
  ls /var/lib/vz/template/iso/ 2>/dev/null | sed "s|^|iso: |"
'
say "left on proxmox:"; pve "qm list; pvesm status | grep local"
