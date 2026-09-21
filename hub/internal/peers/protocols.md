# Three protocols

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
