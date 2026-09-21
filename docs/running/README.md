# Running it

*The hub has to run whether or not anyone is looking at it — heartbeats,
schedules and peers' jobs all happen while the panel is closed.
And it is the cheapest machine in the house running the longest hours,
so it is the thing most likely to die.*

## One small box

The hub is not where the work happens. Models run on the remotes, apps run
on the remotes, pooled disks move data between remotes, agent jobs run on
the remotes — the hub keeps a few hundred kilobytes of state, holds SSH
connections open, copies bytes from one socket to another, and runs the
credential vault and one `omp` process per open session. A session costs
the hub about what a browser tab costs, and a Pi-class hub holds a few of
them. So the floor is set low on purpose: a retired laptop, a NAS that runs
containers, a mini PC and a Raspberry Pi are all the same machine to
HomeDash. The hub is the one machine in the fleet that never runs a
model; a card in it is a card wasted, and belongs
[enrolled as a remote](../pooling/hosts.md) instead.

**Spending more buys you more open sessions, a deeper queue and more
concurrent streams, and nothing else** — no amount of hub is going to make
a token arrive faster, because the model is somewhere else.

## The API

The panel and the CLI both talk to `/api` on the hub; an unknown `/api`
path is a 404, not the panel's HTML.

## The rest of it

- [Installing on Debian](install.md) — the `.deb` that is the hub
  service, the panel and the CLI, and how to reach the panel from
  another machine in the house.
- [Where the state lives](state.md) — one database file, and export and
  restore as the way it survives the hub.
- [Is the hub well](health.md) — the Health page, its thresholds, and
  its API twin for anything watching from outside.
- [Signing in and roles](accounts.md) — passkeys, no identity provider,
  two roles, and the shell on the hub as the one authority that outranks
  the panel.
- [Working it from the command line](cli.md) — the same binary as a
  CLI on your workstation, signed in with your passkey through the
  browser, for you and for whatever assistant you already run.
- [Opening the panel from other machines](https.md) — passkeys need
  HTTPS anywhere but `localhost`; one script puts the panel behind
  `https://homedash.local` with a CA you trust once per device.
- [Keeping it updated](updating.md) — one script that puts an apt index
  in front of the releases page, so a new HomeDash arrives with
  `apt upgrade` instead of when you remember to fetch it.
- [What actually keeps the lab safe](safety.md) — four things, none of
  them a written rule: the hub keeps the keys, a job's account has no
  privilege, root is asked for through one logged door, and a job can be
  rolled back.
