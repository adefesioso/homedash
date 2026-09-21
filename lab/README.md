# lab

A whole neighbourhood on one machine: a Proxmox VE host under libvirt,
and inside it two "houses", each a NATed network with a HomeDash hub and
its remotes. Everything the top-level README describes — enrolling,
placement, storage clusters, the router, an agent job from hub-a to a
remote with credentials brokered over the SSH tunnel, one hub sending
an inference job to another across two NATs, and a service on house B
fronted by house A's hub — can be exercised here without owning a second
machine, and torn down in a minute.

## Shape

```
this machine (libvirt, nested KVM on)
└── homedash-pve            Proxmox VE 9.2, 14 vCPU / 28 GB / 64 GB, 10.20.30.147
    │
    ├── vmbr0  10.20.30.0/24   libvirt NAT to the internet — "the internet"
    │
    ├── nat-a  a NAT router (network namespace): public 10.20.30.201, LAN 10.1.0.1
    │   └── vmbr1  10.1.0.0/24 — house A
    │         101  hub-a          2c / 2 GB               the hub
    │         102  remote-small   1c / 1 GB   +4 GB       Pi-class
    │         103  remote-mid     2c / 3 GB   +8 GB       NAS-class
    │         104  remote-big     4c / 6 GB   +8, +8 GB   the "GPU box" (CPU Ollama)
    │
    └── nat-b  a NAT router (network namespace): public 10.20.30.202, LAN 10.2.0.1
        └── vmbr2  10.2.0.0/24 — house B
              201  hub-b          2c / 2 GB               the peer hub
              202  remote-b       2c / 3 GB   +8 GB       so B has something to serve
```

Three remotes with deliberately different sizes give placement something
to rank and storage clusters two-plus disks to pool. The second house has
its own remote because a hub with no machines is a gateway: it can send a
job but cannot serve one, and cross-house inference needs both sides.

**Each house sits behind a NAT router of its own**, not behind Proxmox.
The router is a network namespace with one leg on the house bridge (the
guests' gateway) and one on vmbr0 with that house's public address,
masquerading and dropping unsolicited inbound exactly like a home router.
Proxmox keeps only a management address on each bridge (`10.x.0.2`) and
forwards nothing between them. So between hub-a and hub-b there is one
"internet" and two port-restricted NATs, and the only way across is a hole
punched from both sides — verified with a plain UDP exchange, and the
shape the DHT rendezvous and DCUtR have to survive. The DHT itself is the
public one, reached through libvirt's NAT.

## Use

```
make up        # proxmox up, bridges configured, six guests booted
make deploy    # build the .deb, install on hub-a and hub-b
make status    # qm list
./ssh.sh pve   # shell on proxmox;  ./ssh.sh remote-big  for a guest
make prune     # remove anything on proxmox that is not this lab
make down      # delete the guests (template stays);  make down-all also stops proxmox
```

`make deploy` prints the two `ssh -L` lines that put each hub's panel on a
local port. Remotes are plain Debian 13 cloud images with the lab key for
`debian`; enrolling them is done from the panel exactly as a real machine
would be, so enrollment is tested too, not bypassed. Three of them were
moved off the image's netplan by hand so the address feature
([hosts.md](../docs/pooling/hosts.md#holding-an-address)) meets every
manager it writes for: remote-small runs ifupdown, remote-big
NetworkManager, remote-b bare systemd-networkd; remote-mid keeps
netplan. Each has a `/etc/systemd/network/10-eth0.link` pinning the
interface name, which netplan's `set-name` used to do — a rebuilt lab
starts on netplan everywhere until this is redone.

## Files

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

## Limits

- The Proxmox installer gives `local-lvm` a 20 GB thin pool, which the
  six guests' disks overcommit five to one; Docker images and Ollama
  models fill it, and a full pool stops every guest with `io-error`.
  `lvextend -L +7g pve/data` takes the volume group's spare space, `qm
  resume` brings the guests back, and `fstrim` on the guests returns what
  they freed. `pve-up.sh` should size the pool larger on a fresh install.
- Between the two houses the hubs find each other on the public DHT and
  talk over a relay within a few minutes; the DCUtR hole punch has not
  been seen to land here, because from the DHT's point of view both hubs
  sit behind three NATs in a row — the house router, libvirt's, and this
  machine's own — and the Peers tab reads "relayed". A cross-house job
  works over the relay within the relay's limits, and so does a service
  fronted as a link: publish a stack's port on hub-b to hub-a, approve it
  on hub-a's Peers tab, and it is a page on hub-a's panel. An ingress
  front cannot be exercised here — there is no public hostname and no
  route from the internet into either house — so it is the one path the
  lab only stands in for.
- No GPU passes through, so Ollama runs on CPU. Use a small model
  (`qwen2.5:0.5b`) — routing, queueing and streaming behave the same.
- An agent job in the lab thinks on a cloud provider signed in on hub-a,
  or on `homedash/qwen2.5:0.5b` through the pool if you want no key
  involved; the install, the tunnel, the event stream and the correction
  loop are the same either way. `remote-big` is the one to send it to, and
  `remote-small` is the one that shows what a 1 GB machine does with the
  agent: enrolls, pools, and refuses the job with the reason.
- The panel needs a passkey. From this machine, `./ssh.sh hub-a -L
  7433:localhost:7433` and register at `http://localhost:7433`; for a
  script, `sudo -u homedash homedash token lab` on the hub prints an admin
  token.
- The Proxmox VM is sized for all six guests running at once with room
  left on a 16-thread / 60 GB host. `lab.env` is the place to shrink it.
- Guests are reachable only through Proxmox's management leg on each
  bridge; nothing is routed from this machine into the houses on purpose,
  because "the outside cannot reach in" is part of what is being tested.
- Anything on Proxmox that is not in the VM table is a leftover;
  `make prune` removes it.
