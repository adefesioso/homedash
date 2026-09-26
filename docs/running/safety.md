# What keeps the lab safe

Controls in code, not rules. Five.

## 1. The hub holds the private things

- SSH key: generated on first start; only the public half leaves.
- Vault: alone holds provider refresh tokens, alone refreshes.
- Secrets: encrypted at rest, handed out by name to the granted host for one job.
- A remote holds a per-host token and an encrypted snapshot — opens nothing elsewhere; **Revoke** makes it open nothing.
- Every connection is pinned to the enrolled host key; a different key reads **mismatch**.

## 2. A remote's agent is root on its own box, and nothing else

A job runs `omp` as root. `homedash` is the hub's account (key, passwordless sudo, used only by the hub's executor). A job may not cut its hub connection, touch the hub, or touch another remote. Held from outside the job:

| Control | Holds |
| --- | --- |
| No credential on a remote opens the hub's API or another remote | Hub, other remotes |
| The job's loopback door serves only the local-only router (no peer model steers root), this host's secrets and vault token | Hub |
| Exports go only to addresses the hub names; a job never makes a cross-machine mount | Other remotes |
| Hold restored after every round — sshd config, hub key, `homedash` sudoers — first by the job unit's inline `ExecStopPost` (runs on timeout, kill, broken sudo), then by the hub over its connection → `host.hold_repaired` | Hub connection |
| Heartbeat reports the hold; host key change reads **mismatch** | Hub connection |
| Hook + fleet addresses rewritten before every round: refusals logged, next job can't be disarmed | All three (visibility) |

Cost: a job bent on cutting the hub off can (stop the network, wipe its disk). The host reads offline; a person goes to it; the [rebuild script](../pooling/agents/rebuild.md) restores it. A root remote reaches what any LAN machine reaches.

## 3. The hub's agent has no hands

Its tools read the fleet, dispatch jobs, run the panel's typed operations. None runs a command or writes a file on a machine.

## 4. One doorway to SSH, gated at the API

- One SSH executor; every value quoted, never concatenated. Published services ride the same connection; nothing on a remote listens.
- Every call that reaches SSH (panel, session, outside assistant, token script) passes one guard.
- Structural refusal: every operation names a host from the database; the hub is not a host, so "here" has no argument.
- List refusal: firewall off, cutting the hub's SSH, editing SSH config, wiping the system disk, deleting a system path, shutdown/reboot.
- Disk rule stops at the system disk (from the heartbeat's `systemDevices`); unreported machine → every disk refused.
- Same list + hold + fleet addresses is the remote's hook: a stated rule and log line; section 2 is the control.
- Compose files meet the docker danger list — see [apps](../pooling/apps.md).
- One implementation for every caller: swapping the assistant doesn't swap the safety.

## 5. Damage is undone, not rebuilt

- Before each round, btrfs or LVM root → snapshot. **Roll back**: live on btrfs, next boot on LVM.
- ext4 → no snapshot, the job says so; the [rebuild script](../pooling/agents/rebuild.md) is the way back.
- Snapshots go when the job is trimmed; a root job can delete one, and rollback says so.
