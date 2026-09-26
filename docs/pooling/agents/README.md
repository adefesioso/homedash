# Working the fleet with agents

*Describing a change is faster than performing it; performing it on six machines, one SSH session each, is an evening.*

Every remote and the hub carry [`omp`](https://github.com/can1357/oh-my-pi). You talk to the hub's agent; it **dispatches**. Each remote's agent is **root on its own machine**, nothing else, and reports what changed.

| Concept | Is |
| --- | --- |
| Session | A named `omp` terminal on the hub, where you talk to the hub's agent |
| Job | Host + directory + text for one remote's agent, run as root, ending in a report |
| Change report | The report's structured part: packages, services, files, mounts, stacks, catalog notes, a proposal |
| Round | One job turn: done, or a correction; capped |
| Hub's hold | sshd, the hub's key, its account's sudo; restored after every round |
| Model | Per remote: fleet default or card override, the house's pool included |
| Rebuild script | Every change report folded into one script per host |
| Proposal | One issue on the project, filed by the hub's agent |
| Usage | Tokens spent by each agent, and served by each machine the router reaches |

## Where each lives

- [Sessions](sessions.md) — the Agents tab; sessions are also the log.
- [Jobs](jobs.md) — dispatch, root remotes, the three limits, change report, loop, rollback, kill.
- [Models](models.md) — defaults, overrides; a peer's model never drives a job.
- [Logs](logs.md) — the remote's session file and the hub's copy of every event.
- [Credentials and secrets](credentials.md) — the hub is the vault; a remote gets a short-lived credential or a named secret for one job.
- [Rebuild script](rebuild.md) — a dead disk's only record is your memory; the script takes a fresh Debian back to the machine.
- [Usage](usage.md) — who spent what, who served it, who asked; the Usage tab.
- [Proposals](proposals.md) — rough edges reach the project as one redacted, capped issue.
