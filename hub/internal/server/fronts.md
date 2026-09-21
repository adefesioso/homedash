# Service fronts

`fronts.go` is a peer's service, fronted here.

- **Link.** `/~{peer}/{service}/…` is a `httputil.ReverseProxy` whose
  transport dials `Peers.DialService` instead of a socket; the prefix is
  stripped and sent as `X-Forwarded-Prefix`. It needs the front approved
  in this hub's `fronts` table and a signed-in user of either role.
- **Ingress.** While any approved front carries a hostname, `ingress`
  keeps a listener on `:443` with `acme/autocert` (cache in `acme/` under
  the state dir, host policy = those hostnames) and one on `:80` for the
  HTTP-01 challenge, routing by `Host` to the same kind of proxy with no
  sign-in of its own. It starts and stops as hostnames appear and go; a
  bind that fails is one `ingress.failed` event and a retry each minute.
  Binding those ports needs `CAP_NET_BIND_SERVICE`, which the
  [unit](../../packaging/README.md) grants.

Every connection through a front is counted in `peer_bytes` as
`fronted`. A front never serves a service this hub is itself reaching
through a peer: a front is by construction a peer's own service, and a
service row names only a host this hub owns.
