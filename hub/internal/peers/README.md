# internal/peers

Part two. There is no server: hubs find each other over a Kademlia DHT
(`go-libp2p-kad-dht`), rendezvous on `sha256("homedash/" + space name)`
as a provider record, and talk over Noise/TLS on QUIC and TCP with DCUtR
hole punching. Identity is the connection's public key, kept as
`peer_key` in the state dir. `Run` watches every `space.*` setting that
shapes the node: on joins, a change leaves and rejoins, off leaves;
nothing configured, nothing running.

## Two networks

Public (client on the IPFS DHT) vs private (own protocol, own
bootstrap/PSK, optional server mode) — same `space.reachable`/`space.port`
either way. [networks.md](networks.md)

## The gate

Only a hub found at the rendezvous is `known`; `handleOffer` resets a
stream from anyone else without reading it, and nothing else creates a
`peers` row. The DHT connects a hub to hundreds of nodes that are not
HomeDash, and libp2p's identify tells every one of them this host speaks
`/homedash/…`; the gate is what makes that harmless.

Every header is read through a `LimitReader` of a few KB. The host runs
a connection manager (low 64, high 192) and a resource manager scaled to
a small box, with inbound streams per peer capped for each HomeDash
protocol, so a stranger's cost stops at the swarm.

## Three protocols

`offer` (capability greeting), `job` (proxy a chat/generate/embed call
through an approved peer), `tcp` (proxy one raw TCP connection to a
published service) — plus the decayed accept/finish/ttft record `Place`
scores candidates by. [protocols.md](protocols.md)

## Tables

`peers`, `peer_jobs`, `services`, `fronts`, `peer_bytes`.
[tables.md](tables.md)
