# Tables

`peers` — the peer id, the name it offered, approved, max concurrent,
per hour, first and last seen.
`peer_jobs` — every exchange in either direction: model, accepted or the
refusal reason, finished, time to first token.
`services` — a published service: name, host, port, and the peer ids it
is published to.
`fronts` — a peer's service this hub serves: peer id, service name,
approved, and a hostname when it is an ingress rather than a link.
`peer_bytes` — bytes copied per peer, service and hour, `fronted` (this
hub fronted the peer's service) or `origin` (the peer fronted this
hub's).
