# Remote jobs

## Start

`Jobs.Start` refuses: memory under the floor, no agent, no agent account (re-provision), cwd missing (`sudo test -d`). Then a `jobs` row and one round in the background.

## A round

One SSH connection, forwards up, in order:

1. `snapshot.sh snapshot <id>` as root → btrfs, LVM or `none` on the row.
2. `Fleet.ArmJob` → hook + fleet addresses ([job door](../fleet/job-door.md#the-hold-around-a-root-job)).
3. `omp --mode json -p --cwd <dir> --model <m> --approval-mode yolo --max-time <t>` in `sudo systemd-run`, **root**, `HOME=/home/homedash-agent`; props `TasksMax`, `RuntimeMaxSec`, `ExecStopPost` (hold restore), optional `jobs.memory_max`/`jobs.cpu_quota`; prompt on stdin.
4. `Fleet.CheckHold` on the same connection → `hold` event line if restored or broken.
5. `parseChanges`: last `homedash-changes` block → `jobs.changes` (compact JSON); none keeps the previous round's; malformed → `stderr` event line.

- Model: host override → `agent.remote_model` → `agent.default_model`.
- Prompt prefix: root here, the three limits, the report and its keys, this host's secret names.
- Each omp JSON line → `job_events`, batched (250 ms or 50 lines, one transaction). Last assistant message = report. `message_end` usage → `usage` row.
- `Correct` = `--resume <session>`, capped by `agent.rounds` (3) → `needs_you`.
- `Rollback` = `snapshot.sh rollback <id>`. Retention `jobs.retention` (20) per host; a trimmed job's snapshot goes with it.

## Kill

Marks the row `killed` (guarded `WHERE state = 'running'`), then `systemctl stop --no-block homedash-job-<id>-r<round>`. The round's own `EndJob` then no-ops on the same guard. Refused if offline or not running.

## Tables

| Table | Holds |
| --- | --- |
| `jobs` | host, cwd, window, model, text, rounds, state (`running`/`done`/`failed`/`needs_you`/`killed`), timeout, session, snapshot (`btrfs`/`lvm`/`none`/`restored`), report, changes, reason, started/ended |
| `job_events` | one row per line, in order |
| `proposals` | filed issues: title, link, when; the daily cap counts 24 h |
| `usage`, `usage_hourly` | per reply / per host-hour tokens and cost; rolled up like `metrics` ([heartbeat](../fleet/heartbeat.md)) |
