# What actually keeps the lab safe

Four things, none of which is a written rule.

**The hub holds the private things, and they never leave.** Its SSH key,
generated on first start, of which enrollment carries only the public
half; the credential vault, which alone holds a provider's refresh token
and alone performs a refresh; and the house's secrets, encrypted at rest
and handed out by name, to the host they were granted to, for the length
of a job. A remote holds what a short-lived, per-host token and an
encrypted snapshot are worth, and no more — a token lifted from one
remote opens nothing on another, and **Revoke** on a card makes it open
nothing at all. Every connection is pinned to the host key enrollment
recorded, so a machine presenting a different one is refused and surfaced
as a mismatch rather than quietly trusted.

**A remote's agent is not root; docker is the one privilege it holds.**
Enrollment makes two accounts. `homedash` is the hub's hands: it holds
the key and passwordless sudo, and only the hub's SSH executor ever runs
as it. A job runs as `homedash-agent`, a second account with no sudo and
no privilege of its own beyond the `docker` group, inside a systemd unit
the kernel enforces: `NoNewPrivileges` (a setuid binary elevates
nothing), the system read-only (`ProtectSystem=strict`), a private
`/tmp`, a task cap (a fork bomb is a kernel refusal, not a pattern
match), a wall-clock limit, and write access to exactly three kinds of
place — its own home, and whatever you mount under it, which is where a
remote's persistent working space goes; the working directory the job
named; and the house's shared storage, the cluster this remote is the
gateway of and every workspace it is a member of. Everything that is the
hub's hold on the machine — sshd and the hub's key (in a root-owned
drop-in, not under any writable home), sudoers, the two accounts, the
firewall, the refusal hook, the helpers — is root-owned, immutable where
the filesystem allows, and out of the agent's reach by permission rather
than by pattern. The same account is caged on the network: loopback and
the internet are open; the house's LAN, the hub's address and every
other remote are refused by an nftables rule on the agent's uid, so a
remote's agent is confined to its own box by the kernel and not by the
absence of a tool.

Docker is the stated exception, and it is worth saying what it costs. A
member of the docker group can ask the daemon for a container that runs
as root with `/` mounted inside it, and the daemon obliges: the group is
root by another name, for anyone who asks the daemon plainly. It is
given on purpose — a job builds, runs and tests containers on its own
machine without a round trip through the hub for every `docker` line —
and what holds it is not the kernel but three things. The refusal hook
on the agent's own tools refuses a privileged container, a host
namespace, an added capability, and a bind of `/`, the docker socket or
a protected path — the same shape `homedash-sudo` refuses at the door —
and the attempt lands in the event log. The per-job snapshot undoes
what got through. And the daemon is that machine's: a container an agent
starts runs there and nowhere else. One thing the cage does not cover:
its rule is on the agent's uid, and a container's traffic is the
daemon's, so a container a job starts sees the house's network as any
container on that machine does.

**Root is asked for, one command at a time, through one door.** A job
that needs a package installed, a service enabled or a disk mounted runs
`homedash-sudo <command>`. That is not sudo: it is a request over the same
loopback door the secrets travel on, answered by the hub, which checks the
command against the refusal list and the privileged allowlist below, runs
it as root over its own SSH connection, records it as an event and in the
job's log, and returns the output. A house that never wants this closes
the door with one setting (`jobs.sudo` off) and a job then has no route to
root at all. The allowlist is a floor with a shape: package managers,
`systemctl`, `docker` without privileged or host-namespace flags, the
ordinary file tools, `mount` — and never a shell, an interpreter, a user
or capability tool, `nft`, `chattr` or `systemd-run`; and no allowed
program may name a protected path (`/etc/ssh`, `/etc/sudoers*`, the hub's
account, the agent's cage, the helpers themselves). The point is not that
root through the door is harmless — a service unit it installs runs as
root, as services do — but that root is never the default, every use of it
is named, checked and logged, and lifting the hub's hold would take a
deliberate, visible, multi-step escalation rather than one careless line.

**Every remote command goes through one doorway, and the gate is in code,
at the API.** A single SSH executor is it, and every value that reaches a
command is quoted, never concatenated. A published service goes through it
too: its bytes are copied to the port over that same connection, and
nothing on the remote listens for them. Every call that can reach SSH —
from the panel, from an agent session, from an outside assistant, from a
script with a token, from `homedash-sudo` — passes one guard first. Two
refusals, different in kind. The first is structural: every operation
names a host and resolves it through the database, and the hub is not a
host record, so there is no argument that means "here". The agent on the
hub has no local tools, so "here" is absent from its toolset as well as
from the call graph. The second is a list: switching off a firewall or
the agent's cage, cutting the hub's own SSH access, editing SSH config by
hand, repartitioning or wiping the disk a machine runs from, deleting a
system path, shutting down or rebooting a machine. These are refused
however they're asked for. The disk rule stops at the system disk on
purpose: formatting a data disk, mounting it and writing its `/etc/fstab`
line is work a remote's agent does on the hub's instructions, and the
heartbeat tells the gate which disk is which (the sources of `/`, `/boot`,
`/var`, `/home` and swap, and the whole disks beneath them); a machine
that hasn't reported that yet has every disk refused.
The same list is installed with the agent on every remote as a hook, so a
remote's agent meets it on its own tools too — there it is visibility,
since the account cannot do those things anyway; the refusal is what the
agent sees, and what lands in the event log.

Putting the gate at the API rather than inside an agent loop is what makes
it worth trusting: there is one implementation, it doesn't care who is
calling, and swapping your assistant doesn't swap your safety.

The same danger list reappears read from a compose file instead of a
command line, for an agent deploying a stack — see
[apps](../pooling/apps.md).

**And when the job did damage anyway, it is undone, not rebuilt.** Before
every job round, if the remote's root filesystem is btrfs or on LVM, the
hub takes a snapshot; **Roll back** on the job restores the machine to
the moment before it — live on btrfs, at the next boot on LVM. On plain
ext4 there is no snapshot and the job says so; the
[rebuild script](../pooling/agents/rebuild.md) is then the way back, from
a fresh Debian. Snapshots go when the job is trimmed. The panel, the
sessions and an outside assistant are three front doors onto one
implementation, none holding a key or a hostname it didn't read from the
hub — swapping any of them changes nothing about what the lab refuses.
