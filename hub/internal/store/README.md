# internal/store

The one database file: `homedash.db` in the state directory, SQLite via
`modernc.org/sqlite` (no cgo), WAL mode, foreign keys on. An absent file
is created on first start; every schema block is `CREATE … IF NOT
EXISTS` and applied on every start, so there is no migration tooling.

Two pools on the one file: `DB`, one connection, is the single writer;
`RO`, a few read-only connections, is what every list and lookup reads
through, so the panel's polling and the router's placement never queue
behind a heartbeat's write. WAL is what makes that safe. Commits are
`synchronous=NORMAL`: a hub crash loses nothing, a power cut can lose
the last moments — which for this data is a metric row or a job line.
Writes that come in bursts go in as one transaction: `Sight` is one
upsert per device and a scan's `Merge` is one commit; a job's lines are
batched by the [agent](../agent/README.md).

Each feature's tables live in a file of this package named for it, and
the rows are read and written only through the methods there. The
packages that own the behaviour document the meaning of their tables:

| File | Tables | Owner |
| --- | --- | --- |
| `store.go` | `events`, `settings`, `windows`, `jobs`, `job_events` | [server](../server/README.md), [agent](../agent/README.md) |
| `hosts.go` | `hosts`, `enroll_codes`, `metrics`, `metrics_hourly` | [fleet](../fleet/README.md) |
| `secrets.go` | `secrets` | [fleet](../fleet/README.md#secrets) |
| `tasks.go` | `tasks`, `task_runs`, `catalog` | [tasks](../tasks/README.md), [apps](../apps/README.md) |
| `clusters.go` | `clusters`, `cluster_members`, `workspaces`, `workspace_members` | [storage](../storage/README.md) |
| `auth.go` | `users`, `credentials`, `sessions`, `invites`, `tokens` | [auth](../auth/README.md) |
| `peers.go` | `peers`, `peer_jobs`, `services`, `fronts`, `peer_bytes` | [peers](../peers/README.md) |

Two things here are the structural half of the gate: `Host(ref)` is how
every remote operation resolves a name or id, and an unknown one is
`ErrNoHost` — the hub itself is never a row, so no argument means
"here". And `ReadSecret` is the only decryptor of a secret value, called
from the job door and nowhere else; the panel and the tools see names.

`events` is the log of transitions; `settings` is key/value, and the
[server](../server/README.md) lists the keys the panel may touch.
