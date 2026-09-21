# Where the logs live

The remote keeps its own session file, as `omp` always does. The hub keeps
its copy of every event and every report, in the database, under the job
— so a conversation is readable while it runs and after, with the remote
off. The Agents tab lists the hub-side sessions; the Jobs tab lists every
job with its report and stream — the running ones always in view, the
finished ones folded to the most recent with the rest one click away. Job
history is trimmed on a retention you set, and the last few per host
always stay; **Clear history** on the Jobs tab removes every finished job
now, with its events and its snapshot on the remote, and never a running
one. What a trimmed or cleared report changed is not lost — see
[rebuild](rebuild.md).
