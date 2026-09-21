# Files

| File | What it does |
| --- | --- |
| `lab.env` | Every fixed fact: names, sizes, addresses, the VM table. Change the lab here. |
| `lib.sh` | `pve` and `guest` SSH helpers (guests are reached by jumping through Proxmox), `each_vm`. |
| `pve-up.sh` | libvirt network + Proxmox VM: defines and unattended-installs it if absent, sizes it, starts it, checks nested virt. |
| `pve-network.sh` | Adds vmbr1/vmbr2 to `/etc/network/interfaces` on Proxmox and installs `homedash-lab-nat`, which builds the two namespace routers on `ifup`. Idempotent; re-running replaces the section. |
| `vms.sh` | Builds the Debian 13 cloud-init template once, clones each VM row, waits for SSH. Idempotent. |
| `deploy.sh` | `make deb`, scp to both hubs, `apt install` (reinstalling the same dev version), health check. |
| `prune.sh` | Proxmox serves only this lab: removes every VM not in the table, plus orphan disks. Asks first; `--yes` skips. |
| `down.sh` | Destroys the guests; `--all` also shuts Proxmox down. Never deletes the qcow2. |
| `ssh.sh` | Shell or one-off command on Proxmox or any guest by name. |

## What lives outside the repo

`~/.homedash/` holds the Proxmox ISO, the qcow2, the unattended-install
answer file with the root password, and the lab SSH key. `pve-up.sh`
creates all of it on a fresh machine given the ISO and
`proxmox-auto-install-assistant`; on this machine it already exists.
