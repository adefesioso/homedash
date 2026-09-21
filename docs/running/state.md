# Where the state lives

State is **one database file** on the hub — hosts, clusters, workspaces,
catalog, history, tasks, peers, services, accounts, agent jobs and their
logs. No database server, no migration tooling, nothing to configure: an
absent file is created on first start. Beside it, in the same directory,
sit the two things `omp` keeps for itself: the credential vault and the
hub-side session files.

The way state survives the hub is **export**, not replication. Settings
exports the state directory as one file, encrypted under a passphrase
you type, and your browser downloads it; keep it wherever you keep
things — a USB stick, a NAS share, a friend's box. What it protects is
the part you can't rebuild by looking at the machines: which hosts are
enrolled, their pinned host keys and rebuild scripts, your clusters, your
peer approvals, your accounts and passkeys, and the vault — every
provider you signed in to. HomeDash never copies your files anywhere you
didn't ask, and a storage cluster's files are not part of an export:
pool what you can re-download.

**Restore** puts that file back. A fresh hub's setup page takes it
before any passkey exists, so a rebuilt hub comes back with the same
accounts, hosts and vault; Settings takes it on a running hub too. The
hub stages the file, checks the passphrase, then restarts itself with
the restored state. On the hub's own shell, `homedash export FILE` and
`homedash restore FILE` (with the hub stopped) do the same — see the
[entry point](../../hub/cmd/homedash/README.md#the-shells-authority).
