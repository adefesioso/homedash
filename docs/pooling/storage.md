# Storage

## Pooling the disks

*One disk is full, another is empty, and the thing you want to store is
bigger than any single drive in the house.*

A **storage cluster** is a set of mountpoints on enrolled remotes
presented as **one filesystem at one path**, with the capacity of all of
them added together. Two or more members, each a host + path pair, plus a
**gateway** — the enrolled machine that path appears on. Apps read and
write that one path and never learn there was more than one disk behind
it.

The hub sets the mount up and then leaves the data path. Each member keeps
its own filesystem and the files already on it, and goes on being an
ordinary readable disk if the cluster is ever torn down. **This is the one
place your remotes trust each other**: a pooled drive can't route every
read through the smallest machine in the house, so each member is exported
to exactly one address — the gateway's — and the hub removes that export
when the member leaves.

A cluster is capacity, not safety:

- A new file lands on the member with the most free space, so a cluster
  fills evenly.
- **A file is never split across members.** A single file still has to
  fit inside one part.
- **A member that's off is a hole in the namespace** — its files are
  missing until it returns, and everything else still reads. A member
  whose disk dies takes its share of the files with it. Pool what you can
  re-download; back up what you can't.
- A dead member returns an error instead of hanging every process that
  touches the mount. The cluster reads **Degraded** and names the member.

A pooled drive puts a live mount over disks you care about, so a member
that carries the system, swap or the cluster's own mount is
[refused in code](../running/safety.md), and removing a member rebuilds
the mount without it while leaving its files exactly where they are. The
Storage tab is a map: a ring for the house's whole disk (clusters,
free to pool, system, unformatted), every machine's disks on the left,
the clusters on the right with a ring each of capacity by member, and a
line from each member disk to its cluster in that cluster's colour —
dashed red while unreachable. It marks which disks are off-limits and
which host reporting them is offline (its rows are stale, not gone).
A raw disk — one with no filesystem on it at all — has nothing to
measure free space on, so it is listed as **unformatted — give this
host a job to format and mount it** rather than left out or offered as
a member. "Put ext4 on /dev/sdb, mount it at /mnt/sdb, make it come back
at boot" is a job the remote's agent can do, because the
[gate](../running/safety.md) refuses disk tools only on the disk the
machine runs from; the next heartbeat then lists the new mount as a
member you can pool.

## A shared workspace

*Each remote's agent works alone on its own disk. The moment a task needs
two of them — one gathers, one builds; one writes a script, another runs
it where the hardware is — the only channel between them is the hub's
agent reading a report and pasting it into the next job. Anything bigger
than a paragraph is lost in the retelling.*

A **shared workspace** is a directory on a cluster that appears at the
same path on every remote you name, so [jobs](agents/jobs.md#a-job) on
different machines work the same files. It is the option you reach for
when a job on one remote should hand something to a job on another
without the hub's agent carrying it: notes, findings and data go in as
files, and so do runnables — a script one remote wrote, a binary another
one built, a dataset a third prepared — and the next job runs them where
they are. The hub's agent still directs; what it no longer has to do is
relay.

Pointing a job at a workspace is a matter of naming its path as the
working directory. The hub's agent creates and shares workspaces and
pools disks jobs formatted; a [job](agents/jobs.md#sharing-between-remotes)
never mounts another remote. Jobs on separate remotes can run
at once on the same workspace, and a job that follows another finds what
it left. The
cluster's rules carry over unchanged: a file lives on one member, a
member that is off is a hole, and a workspace is capacity, not a backup.

Sharing a workspace widens the one place remotes trust each other: the
cluster's gateway shares that directory with exactly the remotes you
named, and nothing else on the cluster. Removing a remote from the
workspace closes that share; the files stay. A workspace never crosses a
[space](../sharing/README.md) — a job from a peer runs on its own disk.
Workspaces are made and edited on the Storage tab, under their cluster,
or by the hub's agent.
