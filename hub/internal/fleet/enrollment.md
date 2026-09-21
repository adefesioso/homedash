# Enrollment

`NewCode` mints a single-use code (fifteen minutes, one use) and the line
to paste: `curl -fsSL http://<hub LAN addr>:7434/enroll/<code> | sudo sh`.
`Script` renders `enroll.sh` for a live code: the **layout** (`layout.sh`,
idempotent, everything below) and then a report — name, SSH port, host
key and facts POSTed back. `Report` records the host with the address the
report came from as the pinned address, and `finish` does the rest over
the connection the hub can now open: mint and deliver the host's vault
token, pull the first credential snapshot, run the rebuild script if the
code named a host to rebuild from, and settle the card with a real sweep.
`Reprovision` pipes the same layout over SSH as root to a host already
enrolled — no code, no new record — and then delivers credentials again;
it is how a remote from before this layout, or behind a pinned version,
catches up.

## What enrollment leaves on a remote

Two accounts. `homedash` is the hub's hands: passwordless sudo, the
docker group, and the hub's key — in `/etc/ssh/authorized_keys.d/homedash`,
root-owned and immutable, named by an sshd drop-in, so the hub's access
depends on nothing under a writable home. `homedash-agent` is the job's
account: no sudo, the `homedash` group (for shared storage) and the
`docker` group (so a job runs containers on its own box — root by
another name, held by the hook, not the kernel; see
[safety](../../../docs/running/safety.md)), and the agent's state:

```
/usr/local/bin/{omp,llmfit,homedash-secret,homedash-sudo}   root-owned
/etc/homedash/agent-cage.nft + homedash-agent-cage.service  the network cage, on at boot
/home/homedash-agent/.omp/
  auth-broker.token                       this host's own vault token, delivered over SSH
  agent/config.yml                        modelRoles.default: the fleet default, or the card's override
  agent/models.yml                        the hub's router as provider "homedash", via the job door
  agent/hooks/pre/homedash-refusals.ts    the refused-command list, as an omp hook; root-owned
  cache/auth-broker-snapshot.enc          omp's encrypted credential snapshot
  agent/sessions/                         the remote's own job sessions
```

The cage is one nftables table on the agent's uid: loopback and public
addresses pass, private ranges (the house, the hub, the other remotes)
are rejected. omp is a Bun binary needing roughly a gigabyte to run; a
remote under about 1.5 GB enrolls and pools but the agent is unusable
there, and the card says so.
