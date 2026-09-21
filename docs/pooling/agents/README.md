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

## The concepts

- **A session** is a terminal on the hub, running `omp`, where you talk
  to the hub's agent — named with a passphrase, listed live or finished.
- **A job** is instructions for one remote's own agent — a host, a
  working directory, and the text — run under a locked-down account and
  ending in a report.
- **A round** is one job turn: the hub's agent reads the report and
  either calls it done or sends a correction, capped before it waits
  for you.
- **A model** is set per remote — a fleet default, or a card's own
  override, including the house's own GPU pool as a provider.
- **The rebuild script** is the standing record every job's report gets
  folded into, so a remote can be recreated from a fresh Debian.

## What it does

- [Sessions](sessions.md) — the Agents tab: named terminal sessions on
  the hub, reattachable, kept live or finished, that are also the log.
- [Jobs](jobs.md) — what the hub's agent may do directly versus send as
  a job, the job's own locked-down account and its three writable
  places, root through `homedash-sudo`, the correction loop, snapshots
  and rollback, and re-provisioning a remote enrolled before this layout.
- [Models and providers](models.md) — the fleet default, per-remote
  overrides, and a connected peer's models in the same picker.
- [Where the logs live](logs.md) — the remote's own session file and the
  hub's database copy of every event and report.
- [Credentials and secrets](credentials.md) — a job needs provider logins
  and the house's tokens, and the only places they could otherwise live
  are a remote's disk or your prompt. The hub is the vault; a remote gets
  a short-lived credential or a named secret for the length of a job.
- [What a remote can be rebuilt from](rebuild.md) — a remote is the sum
  of everything ever done to it, and when its disk dies the only record
  is your memory. One shell script per host, folded from every job's
  report, takes a fresh Debian back to this machine.
