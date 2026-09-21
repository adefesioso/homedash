# internal/tasks

The schedule. `robfig/cron/v3` runs in-process on five-field cron lines
and the `@hourly`/`@daily` words; the hub holds every task and fires it
down the SSH path, so nothing is installed on a remote.

`Save` validates first — the schedule parses and the command passes the
[gate](../gate/README.md) — then stores and (re)schedules. `Fire` runs a
task now, by the clock or by hand: on its host, or on every enrolled
remote one at a time, never in parallel. A task already running is not
started again; an offline host is skipped and the skip recorded; the
command is checked against the gate again on every fire, so one that
became refusable stops; a per-task timeout ends a hanging command. A
task's first failure and its recovery flip `failing`, and the flip is
the notification.

## Tables

`tasks` — name, host (null for every remote), command, cron schedule,
timeout, enabled, `failing`. `task_runs` — per fire per host: started,
ended, exit code, the last 64 KB of output, or why it was skipped; the
newest fifty per task stay.
