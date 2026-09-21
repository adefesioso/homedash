#!/bin/sh
# The snapshot before a job, and the way back. Piped over stdin as root:
# `sh -s -- snapshot|rollback|delete <job id>`. Prints the kind on stdout:
# btrfs, lvm, none; restored or merge-at-boot after a rollback.
set -u
op=$1; id=$2
dir=/.homedash-snapshots
fs=$(findmnt -no FSTYPE / 2>/dev/null); src=$(findmnt -no SOURCE / 2>/dev/null | sed 's/\[.*\]$//')
vg=""; lv=""
if command -v lvs >/dev/null 2>&1 && [ -n "$src" ]; then
  vg=$(lvs --noheadings -o vg_name "$src" 2>/dev/null | tr -d ' ')
  lv=$(lvs --noheadings -o lv_name "$src" 2>/dev/null | tr -d ' ')
fi
case "$op" in
snapshot)
  if [ "$fs" = btrfs ] && command -v btrfs >/dev/null 2>&1; then
    mkdir -p "$dir"
    btrfs subvolume delete "$dir/job-$id" >/dev/null 2>&1 || true
    if btrfs subvolume snapshot -r / "$dir/job-$id" >/dev/null 2>&1; then echo btrfs; exit 0; fi
  elif [ -n "$vg" ] && [ -n "$lv" ]; then
    lvremove -f "$vg/homedash-job-$id" >/dev/null 2>&1 || true
    free=$(vgs --noheadings --units m -o vg_free "$vg" 2>/dev/null | tr -d ' m' | cut -d. -f1)
    size=$(( ${free:-0} / 2 )); [ "$size" -gt 4096 ] && size=4096
    if [ "$size" -ge 512 ] && lvcreate -s -n "homedash-job-$id" -L "${size}m" "$vg/$lv" >/dev/null 2>&1; then echo lvm; exit 0; fi
  fi
  echo none ;;
rollback)
  if [ -d "$dir/job-$id" ]; then
    # Live restore of everything but what the kernel, the runtime, the
    # pooled disks and running containers own.
    if rsync -aAXH --delete --exclude=/dev --exclude=/proc --exclude=/sys --exclude=/run --exclude=/tmp \
        --exclude=/mnt --exclude=/media --exclude=/boot/efi --exclude="$dir" --exclude=/var/lib/docker \
        --exclude=/var/log --exclude=/swapfile --exclude=/swap.img "$dir/job-$id/" / ; then echo restored; exit 0; fi
    echo "restore from $dir/job-$id failed" >&2; exit 1
  fi
  if [ -n "$vg" ] && lvs "$vg/homedash-job-$id" >/dev/null 2>&1; then
    # A merge into the root volume takes effect when it is next activated: at boot.
    if lvconvert --merge "$vg/homedash-job-$id" >/dev/null 2>&1; then echo merge-at-boot; exit 0; fi
    echo "lvconvert --merge failed" >&2; exit 1
  fi
  echo "no snapshot for job $id" >&2; exit 2 ;;
delete)
  [ -d "$dir/job-$id" ] && btrfs subvolume delete "$dir/job-$id" >/dev/null 2>&1
  [ -n "$vg" ] && lvremove -f "$vg/homedash-job-$id" >/dev/null 2>&1
  exit 0 ;;
*) echo "usage: snapshot|rollback|delete <job id>" >&2; exit 2 ;;
esac
