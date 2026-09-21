# lab

A whole neighbourhood on one machine: a Proxmox VE host under libvirt,
and inside it two "houses", each a NATed network with a HomeDash hub and
its remotes. Everything the top-level README describes — enrolling,
placement, storage clusters, the router, an agent job from hub-a to a
remote with credentials brokered over the SSH tunnel, one hub sending
an inference job to another across two NATs, and a service on house B
fronted by house A's hub — can be exercised here without owning a second
machine, and torn down in a minute. The shape of the two houses and their
NATs is in [topology.md](topology.md).

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
local port.

## Where the rest is

- [Topology](topology.md) — the VM table, the NAT routers each house
  sits behind, and the network manager each remote runs.
- [Files](files.md) — what each script does, and what lives outside the
  repo under `~/.homedash/`.
- [Limits](limits.md) — the thin pool, the relay-only cross-house link,
  no GPU, and other things worth knowing before you file a bug.
