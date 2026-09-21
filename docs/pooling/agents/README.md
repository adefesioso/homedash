# Working the fleet with agents

*The panel shows the whole house and you still do the work on it one SSH
session at a time. Describing a change — "move the photo library to the
new disk and repoint Immich at it" — is faster than performing it, and
performing it on six machines is an evening.*

Every enrolled remote carries a coding agent —
[oh-my-pi](https://github.com/can1357/oh-my-pi), `omp`, installed by
enrollment — and the hub carries one too. The one on the hub is the one
you talk to. It sees the fleet; the ones on the remotes see their own
machine. You describe the outcome once; the hub's agent turns it into
instructions for each remote's agent, the remotes do the work on
themselves, and the hub's agent reads what came back.

## Sessions

The **Agents tab** is a set of terminal sessions, each an `omp` process
running on the hub. Open as many as you want — the hub names each one
with a passphrase of its own, `bounty-koala`, so there is nothing to type
and no two live sessions share a name; close the browser and they keep
running; open it again and each session reattaches where you left it.
Sessions are kept on the hub, so the tab's list — every session, live or
finished, with its last activity — is also the log. The live sessions are
always in view; the finished ones fold to the most recent, with the rest
one click away. A finished session keeps the last screenful of what it
showed: **History** on its row opens it, read-only, and closes it again
(a session a hub restart ended has none — the process took its screen
with it). A finished session stays until you
**Delete** it, or **Clear history** removes every finished one at once;
the jobs a session started keep their record on the Jobs tab either way.
An attached session is heavy over a tunnel
or on a phone: `omp` redraws its whole screen constantly, and nothing on
either side throttles it. A viewer's session says read-only: it attaches
and watches the same screen, but its keystrokes go nowhere — the
[role split](../../running/accounts.md) is enforced by the hub, not by
what the panel lets you type.

## What the hub's agent can do

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

## Models and providers

A **fleet default** — provider and model — is set in Settings and applied
to every remote at enrollment. Any card can override it — **Change**
beside **Agent** on the host card opens the same provider-and-model
picker Settings has, with *fleet default* as the first choice — because
a machine with a GPU should think locally and a Pi-class box should not,
and the house's own [pool](../inference.md) is a provider like any other:
point a remote at the hub's router and its agent runs on the fleet's
GPUs. The card shows the model the remote last
reported it was configured for, never what the hub asked for. A model is
always `provider/model` — the model half is the provider's own id and
may itself contain slashes (`openrouter/openai/gpt-4o-mini`).

The `homedash` provider's picker isn't only this hub's own machines: a
model a connected peer currently offers appears too, since the router
already [places a request there](../inference.md#getting-the-models-onto-the-machines)
when nothing local holds it. Picking one is picking the space, not just
the house.

## Where the logs live

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

## The rest of it

- [Credentials and secrets](credentials.md) — a job needs provider logins
  and the house's tokens, and the only places they could otherwise live
  are a remote's disk or your prompt. The hub is the vault; a remote gets
  a short-lived credential or a named secret for the length of a job.
- [What a remote can be rebuilt from](rebuild.md) — a remote is the sum
  of everything ever done to it, and when its disk dies the only record
  is your memory. One shell script per host, folded from every job's
  report, takes a fresh Debian back to this machine.
