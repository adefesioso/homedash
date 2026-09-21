# State on disk

`/var/lib/homedash` (`HOMEDASH_STATE`) holds `homedash.db`, the hub's
key pair, `peer_key`, `secrets.key`, `acme/` for ingress certificates and
`omp/` — the hub's own omp root, laid out in the
[agent README](internal/agent/README.md#state-on-disk). An export carries
the whole directory, not just the database: the vault is as unrebuildable
as the pinned host keys. A restore staged under `restore.pending/` is
applied at the next start, before the store opens.
