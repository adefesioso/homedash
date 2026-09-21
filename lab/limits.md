# Limits

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
