#!/bin/bash
# `down.sh`        stops and deletes the guests (template kept).
# `down.sh --all`  also shuts the Proxmox VM down. Nothing on disk is removed.
. "$(dirname "$0")/lib.sh"
rm_vm() { pve "qm status $1 >/dev/null 2>&1 && { qm stop $1 >/dev/null 2>&1 || true; qm destroy $1 --purge >/dev/null; echo destroyed $2; }" || true; }
if [ -n "$(pve_ip)" ] && pve true 2>/dev/null; then each_vm rm_vm; fi
if [ "${1:-}" = "--all" ]; then say "shutting down $PVE_VM"; virsh -q shutdown "$PVE_VM" || true; fi
