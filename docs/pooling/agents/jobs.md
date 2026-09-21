# Jobs

## What the hub's agent can do on its own

Nothing on the hub. Its built-in tools are switched off and its only tools
are HomeDash's own — the same fleet the
[CLI](../../running/cli.md) on your workstation reaches, as typed tools
over a small MCP server, since a session has no shell — so what you type into a session passes through
the same [gate](../../running/safety.md) as a stranger's assistant, and
there is no tool by which a session could read a file on the hub or run a
command there.

**Work goes to a remote as a job, by default.** The hub's agent has two
tools that act on a machine directly — `run_command` and `write_file`,
run as the hub's own account, with sudo, through the gate — and it uses
them to look (a file, a status, an exit code), not to do. Anything with
more than one step, anything that writes, anything a remote can decide
better because it can see its own machine, is a job. The direct tools
do work only where a job is impossible: a host with no agent (too small,
or enrolled before the agent account), a machine on the house's network
a job cannot reach, or one root command that a job's own `homedash-sudo`
could not have run for it. When a task is mostly the job's and a small
part is not, the job still does the most part — the hub's agent does the
small one and says which it did and why. A job's limits, below, are what
the hub's agent writes its instructions around: it tells the remote where
it may write and how to ask for root, rather than discovering the refusal
in the report.

## A job

A **job** is instructions for one remote's agent: the host, a working
directory on it, and the text. The **Jobs tab** is where jobs are started
by hand and where every job, from a session or from the API, is read
while it runs and after. A host that isn't online is refused before
anything starts, and so is a directory that isn't already there — the hub
checks both rather than starting a session that would only fail later,
somewhere less visible. The hub then starts `omp` on that remote over
the SSH path enrollment opened, headless, in that directory, with the
model that remote is set to, and copies every event it emits into the
hub's database as it happens. The remote works alone — its own tools, its
own files, its own shell, inside that machine and nowhere else — and the
job ends with a **report**: what it changed, what it could not, and
whether it finished or ran out of the per-job time you set. The working
directory is usually the remote's own disk; it can also be a
[shared workspace](../storage.md#a-shared-workspace), in which case the
job is one of several working the same files.

The job runs as its own account, `homedash-agent`, not as root. It can
write three places and nothing else — the system is read-only to it and
the house's network is closed to it, by the kernel. Its **home** is its
own: anything you mount under `/home/homedash-agent` (a disk, a share, a
directory you want to keep) is that remote's persistent working space,
there for every job and still there after. The **working directory** the
job named. And the house's **shared storage**: the
[cluster](../storage.md) this remote is the gateway of, and every
[shared workspace](../storage.md#a-shared-workspace) it is a member of,
whatever directory the job started in. It reaches loopback and the
internet. It also runs **docker** itself — builds, `docker run`, a
compose file of its own — on its own machine; what it may not ask the
daemon for is a privileged container, a host namespace, an added
capability, or a bind of `/`, the docker socket or a protected path, and
the [refusal hook](../../running/safety.md) says so and logs it. When
the work needs root — a package, a service, a mount — the remote runs
`homedash-sudo <command>`, and the hub answers: the command is checked
against the [gate](../../running/safety.md), run as root over the hub's
own connection, and logged in the job's events. A house that wants no
job to ever reach root turns `homedash-sudo` off in Settings. Stacks the
house runs are still the hub's business — its agent deploys them with
its own tools and the [catalog](../apps.md) — and a job's containers are
the job's: what it needed to do its work, on its box.

Before each round the hub snapshots the remote's root filesystem if it
can — btrfs, or LVM with room for a snapshot — and the job carries the
result: **Roll back** on a finished job puts the machine back to before
it ran (live on btrfs; on LVM at the next boot, which is yours to do). A
plain ext4 root has no snapshot, the job says `none`, and the
[rebuild script](rebuild.md) remains the way back.

## The loop

The hub's agent reads the report and decides: done, or not yet. A
correction is a follow-up turn on the same remote session, so the remote
keeps everything it learned the first time. Rounds are capped — a Settings
value, three by default — and a job that reaches the cap is marked
**needs you** rather than sent round again. You can step in at any round:
the session is yours the whole time.

## Remotes enrolled before this layout

A remote enrolled before the agent account existed has one account and a
job there is refused with the reason. **Re-provision** on the card runs
the enrollment layout again over SSH — the accounts, the root-owned key,
the cage, the helpers, the pinned agent — without a new code and without
a new host record. It is also how a pinned `omp` version reaches a remote
after a hub upgrade.
