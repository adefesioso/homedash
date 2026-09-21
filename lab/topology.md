# Shape

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

## Network managers

Remotes are plain Debian 13 cloud images with the lab key for `debian`;
enrolling them is done from the panel exactly as a real machine would be,
so enrollment is tested too, not bypassed. Three of them were moved off
the image's netplan by hand so the address feature
([hosts.md](../docs/pooling/hosts.md#holding-an-address)) meets every
manager it writes for: remote-small runs ifupdown, remote-big
NetworkManager, remote-b bare systemd-networkd; remote-mid keeps
netplan. Each has a `/etc/systemd/network/10-eth0.link` pinning the
interface name, which netplan's `set-name` used to do — a rebuilt lab
starts on netplan everywhere until this is redone.
