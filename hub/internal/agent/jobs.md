# Remote jobs

`Jobs.Start` refuses a host under the memory floor, without an agent, or
without the agent account (re-provision), records a `jobs` row, and runs
one round in the background. A round is one SSH connection with the
fleet's forwards up; on it, in order: `snapshot.sh snapshot <id>` as root
(btrfs subvolume or LVM snapshot of `/`, or `none`; the kind lands on the
row), then `omp --mode json -p --cwd <dir> --model <provider/id>
--approval-mode yolo --max-time <t>` (the model: the host's own
override, else `agent.remote_model`, else the hub's
`agent.default_model`) — not as the account the hub logged
in as, but inside `sudo systemd-run` as `homedash-agent`, with
`NoNewPrivileges`, `ProtectSystem=strict`, `PrivateTmp`,
`RestrictSUIDSGID`, `TasksMax`, `RuntimeMaxSec`, the optional
`jobs.memory_max` and `jobs.cpu_quota`, and `ReadWritePaths` for the
account's home (and so whatever is mounted under it), the working
directory, the cluster this host is the gateway of and every workspace
it is a member of (`sharedPaths`, each with systemd's `-` prefix so an
unmounted one is skipped, not fatal); the prompt on stdin. Every
JSON line omp emits is a `job_events` row, written in batches (every
quarter second or fifty lines, one transaction) so a chatty job is not
one fsync per line; the last assistant message is
the report; a `sudo` line is appended for every `homedash-sudo` the door
served. An assistant `message_end` line's `usage` (input, output, cache
read/write tokens, cost) is a `usage` row against the host and the job —
the Usage tab's numbers. The hub prepends one paragraph to every job's text: it has no
root, it has docker, what it can write, `homedash-sudo` is how to ask for root (a package, a
service, a mount, a data disk to format and add to fstab — never the system disk), end with a report listing
every change as the commands that would make it again, name data that
should survive a rebuild — plus the names of the secrets this host may
read. `Correct` is `--resume <session>` on the remote's own session,
capped by `agent.rounds` (3); at the cap the job is `needs_you`.
`Rollback` runs `snapshot.sh rollback <id>` on a finished job. Retention
`jobs.retention` (20) is applied per host after every round, and a
trimmed job's snapshot is deleted with it.

`Kill` stops a running job: it marks the row `killed` first (a guarded
update, `WHERE state = 'running'`), then SSHes over and
`systemctl stop --no-block`s the round's own unit
(`homedash-job-<id>-r<round>`). Stopping the unit also ends the round's
`systemd-run --wait`, so the round goroutine's own `EndJob` call lands
after and finds the row already past `running` — same guard, so it's a
no-op rather than overwriting `killed` with `failed`. Refused if the
host is offline (nothing to SSH into) or the job isn't running.

## Tables

`jobs` — host, working directory, the hub-side window that started it,
model, text, round count, state (`running`, `done`, `failed`,
`needs_you`, `killed`), timeout, the remote's session id, the snapshot kind
(`btrfs`, `lvm`, `none`, or `restored`), report, reason, started/ended.
`job_events` — one row per line, in order.
`usage` / `usage_hourly` — one row per reply / per host-hour: input,
output, cache read/write tokens, cost, call count. Rolled up and
trimmed the same way as `metrics` (see
[the heartbeat](../fleet/heartbeat.md)).
