# Tasks

*Half the maintenance in a lab is a command somebody has to remember.
Prune the images, dump the database before the backup runs, pull the model
everyone will want on Monday. Each one ends up in a crontab on one
machine, invisible from the next morning on — so nobody can say what runs
in this house, or whether it worked last night.*

A **task** is a command, a host, and a schedule. The hub holds all three
and runs the command down the SSH path enrollment already opened, so
adding a task installs nothing on the remote and there is no crontab
anywhere to fall out of step with the panel. The **Tasks tab** is the list
of everything scheduled in the house, which is the thing no home lab has.

A task names one remote, or every enrolled remote. A fan-out runs them one
machine at a time: a fleet-wide prune that starts on eight machines at
once is an outage with a schedule attached.

Every fire is recorded — when, how long, exit code, output — and the last
runs stay on the row, because "did that actually work" is the only
question anyone asks of a scheduled job. Runs never stack; an offline host
is skipped and recorded, not retried; a hub that was down over a tick
makes nothing up; a hanging command is ended by a per-task timeout, from 1
to 86400 seconds (a day) — blank keeps the default of 600. A task
that starts failing, and one that recovers, reaches the
[notification target](notifications.md). **Run now** is on every row: a
schedule you can't test is a schedule you find out about in a month — but
not on a disabled task, which Run now refuses, the same as the clock
would.

A task is arbitrary remote execution, and it goes through the
[same gate](../running/safety.md) as everything else the hub can do — on
save and on every fire, so a command that becomes refusable stops running
rather than keeping its grandfather rights.
