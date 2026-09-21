# internal/peers

Part two. There is no server: hubs find each other over a Kademlia DHT
(`go-libp2p-kad-dht`), rendezvous on `sha256("homedash/" + space name)`
as a provider record, and talk over Noise/TLS on QUIC and TCP with DCUtR
hole punching. Identity is the connection's public key, kept as
`peer_key` in the state dir. `Run` watches every `space.*` setting that
shapes the node: on joins, a change leaves and rejoins, off leaves;
nothing configured, nothing running.

## Two networks

`space.network` picks which DHT — [the choice](../../../docs/sharing/network.md).

- **public** (default): client mode on the IPFS DHT through its bootstrap
  nodes. The hub declares itself private up front and reserves slots on
  two relay-v2 peers it finds there, so it is reachable before the hole
  punch lands.
- **private**: the DHT protocol is prefixed `/homedash` so it never
  meets the IPFS one, and the bootstrap peers are the multiaddrs in
  `space.bootstrap`. A hub with `space.reachable` on runs in server mode,
  listens on `space.port`, declares itself public and runs the relay-v2
  hop service for the others; a hub without it is a client and relays
  through the reachable hubs. `space.psk` (64 hex) is a libp2p private
  network key: the swarm handshake fails without it, and the host listens
  on TCP only, since QUIC cannot carry a PSK.

`space.reachable` and `space.port` apply in both modes.

A hub found at the rendezvous is protected from the connection manager
and redialed as soon as its last connection drops (backing off from a
second to half a minute until it is back or the node leaves), because a
public relay resets every relayed connection after two minutes or
128 KB and the minute-long discovery tick alone would leave the peer
disconnected for most of that.

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

- `/homedash/1.0.0/offer` — a greeting each way, refreshed every minute:
  name, models, whether a machine is free, and the names of the services
  published to that peer. A hub with `space.serve` off offers no models.
  Learning an offer upserts the `peers` row with the space-wide defaults
  (`space.unknown`, `space.default_concurrent`, `space.default_per_hour`)
  and a `fronts` row, unapproved, for each service named that this hub
  has not seen from that peer before. `Offered` is the dedup of every
  live, free offer's models — [pool](../pool/README.md)'s seam for
  listing, on the Models tab and to a local model picker, what the space
  can additionally serve; it is never mixed into this hub's own outbound
  offer or into `handleJob`'s check of what to accept, both of which stay
  local-only.
- `/homedash/1.1.0/job` — a header (model, path, body length), a yes or
  a reason, then the body, then the response bytes. `Place` is the pool's
  seam: among approved peers whose offer is under three minutes old,
  free and lists the model, try them best record first; a peer with no
  record is tried. The serving side (`handleJob`) refuses when not
  serving, not approved, over `per_hour`, over the hub-wide
  `space.per_hour`, over `max_concurrent` or the hub-wide
  `space.ceiling`, when the path is not one of `jobPaths` (chat,
  generate, embed — never pull, create, push or copy), when the body is
  over 64 MB, or when nothing in the pool holds the model; otherwise it
  reads the body and serves the request through the local router with
  `X-HomeDash-Local-Only`, which is the one-hop rule. Every exchange is a
  `peer_jobs` row.
- `/homedash/1.0.0/tcp` — a service name, a yes or a reason, then one
  TCP connection's bytes both ways, one stream per connection, so
  WebSockets and long polls need nothing special. The origin side
  (`handleTCP`) refuses a sender that is not approved, a service not
  published to it, a host that is not online, or a connection past
  `space.connections` (64) or `space.peer_connections` (16); otherwise it
  opens a `direct-tcpip` channel on the SSH connection the fleet holds to
  that host and copies, through a per-peer token bucket when
  `space.peer_kbps` is set. Nothing listens on the remote. The front side
  (`DialService`) returns a `net.Conn` the
  [server](../server/README.md) proxies to. Bytes in both directions are
  counted per peer and service by the hour.

## The record and the counts

`PeerRecord` is three decayed numbers from the last week of sent jobs,
each weighted by half per two days of age: the share accepted, the share
that finished, and the median time to first token. `Place` orders
candidates by `0.5·accepted + 0.3·finished + 0.2/(1 + ttft/2s)`.
`PeerCounts` is the score: accepted jobs by model and bytes carried, in
both directions, over a day, a week and all time.

## Tables

`peers` — the peer id, the name it offered, approved, max concurrent,
per hour, first and last seen. `peer_jobs` — every exchange in either
direction: model, accepted or the refusal reason, finished, time to
first token. `services` — a published service: name, host, port, and
the peer ids it is published to. `fronts` — a peer's service this hub
serves: peer id, service name, approved, and a hostname when it is an
ingress rather than a link. `peer_bytes` — bytes copied per peer,
service and hour, `fronted` (this hub fronted the peer's service) or
`origin` (the peer fronted this hub's).
