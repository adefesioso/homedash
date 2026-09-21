# Sharing beyond your house

*Your lab alone is small, and its idle hours are the same hours every
day. Two streets away someone else's is idle at different ones. Neither of
you is going to run a server so the other can borrow capacity — or so
that the things you run at home can be reached from anywhere else.*

So there is no server. HomeDash hubs find each other over a DHT: a
shared **space name** is hashed into a rendezvous, and any hub that typed
the same name is discovered and gets a direct, encrypted, hole-punched
connection. No homeserver, no accounts, no registration — the name is the
rendezvous. The DHT is the public IPFS one by default, or a private one
made only of your hubs — see [the network under a space](network.md).

## The concepts

- **A space** is the set of hubs that typed the same name. Joining is a
  Settings value and an on/off switch; nothing else — no port to forward,
  no public URL, no token to distribute. Hubs behind NAT participate on
  equal terms.
- **A peer** is another hub in your space, identified by the public key
  of its connection — not a claim in a payload, and not forgeable. Only
  a hub found at the rendezvous is a peer: a node that merely connects
  and greets is ignored.
- **An offer** is what every connection is greeted with, refreshed every
  minute: the models this hub can serve, whether it currently has a free
  machine, and the services it has published to that peer. A hub
  advertises only the machines it owns, and a service only to the peers
  it was published to. A connection closing drops its peer immediately,
  and stale offers stop counting.
- **One hop.** Whatever arrives from a peer — a job or a connection — is
  served on this hub's own machines or refused, never passed on. The
  inbound path has no route to the peer fallback at all, so the rule is a
  property of the shape, not a check that could be forgotten.
- **Settings values.** Every on/off switch here — Joined, reachable, Run
  peers' jobs — stores `true` when checked and blank when not; that's the
  only vocabulary a save writes, though a value from before this still
  reads as on.

## What it does

- [The network under a space](network.md) — the public DHT and what it
  gives away, and how to run a private one when that is too much.
- [Inference across the space](jobs.md) — how a job travels from one hub
  to another, how the record of what each peer has actually done decides
  which one gets it, and how a hub with no machines of its own becomes one
  door to every lab in the space.
- [Publishing a service](services.md) — one port on one machine you own,
  offered to the peers you name, fronted by them as a page on their panel
  or as a public hostname with a certificate.
- [What a peer decides](peers.md) — approval, quotas and fronts on the
  receiving side; the score of what each hub has taken and given; and
  what sharing costs you in privacy.
