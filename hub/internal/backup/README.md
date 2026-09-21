# internal/backup

How state survives the hub: one file, exported when you ask, restored
into a fresh hub. No target, no interval, no retention — the file is
yours to put where you like.

`Export` writes a consistent copy of the database (`VACUUM INTO`), the
hub's key pair, `secrets.key`, `peer_key` and omp's own files — the
vault (`agent.db`) and the session files, never the binary or caches —
as one tar, encrypted with `filippo.io/age` under a passphrase (scrypt),
to whatever writer asked: the panel's download, or a file on the hub's
shell. The time of the last export is kept (`backup.last`) so Settings
and Health can say how old your latest copy is.

`Stage` is the first half of a restore: it opens the file with the
passphrase, checks that every entry is one of the names an export
writes and that the database is a SQLite file, and lays the tree out in
`<state>/restore.pending/`. Nothing live is touched, so a wrong
passphrase or a corrupt file is refused with no damage. `Apply` is the
second half: it moves the staged files over the live ones (dropping the
database's `-wal`/`-shm` sidecars so SQLite reads the restored file, not
a stale journal) and removes the staging directory. `Apply` runs only
while nothing has the state open — at the top of `homedash serve`,
before the store is opened, and from `homedash restore` with the hub
stopped. That is why the panel's restore stages, then restarts the hub:
the running process never swaps its own database.

A restore brings the accounts and passkeys of the exported hub with it,
which is what makes rebuilding a hub on a new disk possible before any
passkey exists on it: the setup page accepts an export while the hub
has no users yet.
