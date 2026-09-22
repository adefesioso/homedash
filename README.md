# homedash

Most houses have more computers than anyone is using — a replaced
desktop, a NAS, a Pi, a dead-screen laptop with a working GPU — and the
few people who put that hardware to work do it alone, invisible to
someone running the same setup two streets away.

HomeDash **pools the machines in your house** — compute, storage,
inference — into one page, worked through the coding agent each machine
carries, and **serves that pool beyond the house**: inference to other
HomeDash users, services you name, over a network with no server in the
middle. The first is a control panel; the second is the point — a home
lab that becomes a share of infrastructure a community holds together.

## The problem

**Inside one house.** Nothing connects the machines, so each is an
island with its own SSH session, disks and login rules — what's running
where takes a guess, so the lab drifts: disks fill next to empty ones
while a GPU idles.

**Between houses.** That waste repeats at different hours in every
house, and a group with plenty of hardware between them has none in
common — sharing means someone standing up a server everyone depends on.

**Out of the house.** Reaching your own Jellyfin from a train needs a
port forward, impossible behind carrier-grade NAT — so people rent a
tunnel while someone two streets away has a public address unused.

## The boundaries, stated once

**The hub orchestrates; the remotes do the work.** Nothing is computed on
the hub — no model, no container, no agent with hands on it. It keeps a
database, opens SSH connections, copies bytes between sockets, and hosts
sessions whose only tools are the fleet — a machine you already own.

**Pooling is your whole fleet. Sharing is what you published, by name.**
Two things cross to another house, both named first: inference — a
prompt, safe to hand a stranger's machine — and a **service** you
published, one port, offered to peers you chose. Everything else pools
within your own machines. A hub serves an arriving job on its own
machines or refuses it, and never passes it on.

| | Inside your house | Across the space |
| --- | --- | --- |
| Compute | Every enrolled remote | A prompt, one hop, if the peer agreed |
| Network | Every device a remote can see | Never |
| Storage | Pooled into clusters | Never |
| Apps | Placed, run, published | A published service, one hop, through named peers |
| Tasks and agents | Scheduled, run | Never |
| Credentials | The hub's SSH key, brokered provider credentials | None — identity is the connection's public key |
| State | One file on the hub | None — no shared ledger, no registry |

## Three rules that run through all of it

**The machine's word wins.** The panel shows what a remote last reported,
never what the hub asked for — nothing to reconcile or drift.

**A rule is not a control.** Anything that would be a disaster if
ignored is refused in code, not hoped for — see
[what keeps the lab safe](docs/running/safety.md).

**One box, one file, one process.** The hub is one small always-on
machine, held to a floor the spare machine you already have clears.

## Where the rest is

- [Pooling your own machines](docs/pooling/README.md) — joining, apps,
  tasks, storage, GPUs, the fleet's agents.
- [Sharing beyond your house](docs/sharing/README.md) — a serverless
  space of hubs sharing inference and services.
- [Running it](docs/running/README.md) — installing, state, health,
  sign-in, the CLI, what keeps the lab safe.
- [hub/](hub/README.md) — the binary that runs all of it.
- [lab/](lab/README.md) — a neighbourhood on one machine to exercise
  every path above without a second house.
- [tests/](tests/README.md) — an acceptance suite for these claims
  against a live lab, not a mock.
- [mobile/](mobile/README.md) — the Android app a phone pairs into
  the fleet with.
