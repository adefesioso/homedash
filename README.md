# homedash

Most houses have more computers than anyone is using — a replaced
desktop, a NAS, a Pi behind the television, a laptop with a dead screen
and a working GPU — and the few people who put that hardware to work do
it alone, invisible to someone running the same setup two streets away.

HomeDash does two things about that:

1. **It pools the machines in your house** — compute, storage and
   inference — into one set of resources you see and act on from a single
   page, and works them through the coding agent each machine carries.
2. **It serves that pool beyond the house** — its inference to other
   HomeDash users, and the services you choose to publish to anyone you
   name — over a decentralized network with no server in the middle.

The first is a control panel. The second is the point: a home lab that
stops being a solo hobby and becomes a share of infrastructure a
community holds together.

## The problem

**Inside one house.** Nothing connects the machines, so each is an island
with its own SSH session, disks, Docker and login rules. Ordinary
questions — what's running where, which machine has room, who can log
into that box, is it even on — take an SSH session and a guess, so you
stop asking and the lab drifts: disks fill next to empty ones and a GPU
idles twenty-two hours a day while you pay per token somewhere else.

**Between houses.** That waste repeats independently in every house. The
GPU idle here is idle at a different hour than the one across town, and a
group that owns plenty of hardware between them has none in common,
because offering anything to another lab means someone standing up a
server, holding accounts, and becoming the thing everyone depends on.

**Out of the house.** Reaching your own Jellyfin from a train means a port
forward, a domain, a certificate and a router you can configure — and
behind carrier-grade NAT the port forward isn't yours to make — so people
rent a tunnel and the lab depends on that company from then on. Meanwhile
someone two streets away has a public address and nothing behind it.

## The boundaries, stated once

**The hub orchestrates; the remotes do the work.** Nothing is computed on
the hub for the lab — no model, no container, no file served from a pooled
disk, and no agent with hands on the hub itself. It keeps a small
database, opens SSH connections, copies bytes between sockets, and hosts
the sessions you type into; those sessions have no tools but the fleet.
That is what lets the hub be a machine you already own and aren't using.

**Pooling is your whole fleet. Sharing is what you published, by name.**
Two things cross to another house, both named before they do: inference —
a prompt is a small, self-contained unit of work, safe to hand to a
stranger's machine and simple to count — and a **service** you published:
one port on one machine you own, offered to the peers you chose.
Everything else pools within the machines you own and is never offered to
a peer. Nothing crosses because it exists; it crosses because you named it.

**Work crosses that line once.** A hub serves an arriving job or
connection on its own machines or refuses it, and never passes it on.

| | Inside your house | Across the space |
| --- | --- | --- |
| Compute | Every enrolled remote | A prompt, one hop, if the peer agreed |
| Network | Every device a remote can see, wired, Wi-Fi or Bluetooth | Never |
| Storage | Pooled into clusters | Never |
| Apps | Placed, run, published | A service you published, one hop, through the peers you named |
| Tasks and agents | Scheduled, run | Never |
| Credentials | The hub's SSH key, and the provider credentials the hub brokers to its remotes | None. Identity is the connection's public key |
| State | One file on the hub | None. No shared ledger, no registry, no server |

Work leaves the house one way, and **it is not a default**. A space you
have not joined is absent, a service you have not published is invisible,
and a HomeDash with nothing configured is a control panel for your own
machines that talks to nobody.

## Three rules that run through all of it

**The machine's word wins.** The panel shows what a remote last reported
about itself, never what the hub asked for. There is no desired state to
reconcile and nothing to drift.

**A rule is not a control.** Anything that would be a disaster if ignored
is refused in code, not written down and hoped for — see
[what keeps the lab safe](docs/running/safety.md).

**One box, one file, one process.** The hub is one small always-on machine
with one database file and one hub process, held to a floor low enough
that the spare machine you already have clears it.

## Where the rest is

- [Pooling your own machines](docs/pooling/README.md) — part one: joining
  machines, seeing everything else on the house's network, running apps,
  scheduling tasks, pooling disks and GPUs, and working the whole fleet
  through its agents from one seat.
- [Sharing beyond your house](docs/sharing/README.md) — part two: a
  serverless space of hubs that run each other's inference and front each
  other's published services, each peer deciding what it accepts.
- [Running it](docs/running/README.md) — the one small box, installing on
  Debian, opening the panel from other machines and keeping it updated,
  where state lives and how it survives,
  whether the hub itself is well, signing in, driving it from the command
  line, and what actually keeps the lab safe.
- [hub/](hub/README.md) — the binary that runs all of it: the stack, the
  layout, and how to build and package it.
- [lab/](lab/README.md) — a whole neighbourhood on one machine, for
  exercising every path above without owning a second house.
- [tests/](tests/README.md) — an acceptance suite that runs the claims
  above against a live lab, not a mock.
