# Jobs

## The hub's agent dispatches

- Tools: read the fleet (hosts, storage, apps, jobs, catalog); start, correct, kill, roll back jobs; the panel's typed operations (deploy a catalog stack, lock, pool a disk, share a workspace, rebuild script, catalog, [proposal](proposals.md)).
- No tool runs a command or writes a file on a machine. Work on a machine = a job, written as an outcome.
- A host with no agent (too small, old layout) is beyond it; panel and [CLI](../../running/cli.md) still reach it.

## A job

- Host + working directory + text. Started from the Jobs tab, a session or the API.
- Refused: host offline, directory missing.
- Runs `omp` headless **as root** in a unit: wall clock, task cap, Settings' memory/CPU caps; the host's model; every event copied to the hub live.
- Root = does the work itself: packages, drivers, services, any docker, data disks. No allowlist, no asking.

| A job may not | Held by ([safety](../../running/safety.md)) |
| --- | --- |
| Cut its hub connection (sshd, hub key, `homedash` + its sudo, reboot) | Unit's stop-post restores the hold on any stop; hub re-checks and records it; heartbeat; host-key **mismatch** |
| Touch the hub | No hub credential on a remote; door serves only router, own secrets, own vault token |
| Touch another remote | No remote holds another's key; exports only to hub-named addresses; hook refuses fleet addresses |

Hook and fleet addresses are rewritten each round. The left column is stated and refused by the hook; the right column is the control.

## The change report

The report ends with a fenced `homedash-changes` JSON block, parsed onto the job:

| Key | Holds |
| --- | --- |
| `packages`, `services`, `files`, `mounts` | Commands that make each change again |
| `stacks` | Compose projects started, stopped, changed |
| `catalog` | `{entry, note}`: an entry proven wrong, or a stack worth saving |
| `data` | Paths that must survive a rebuild |
| `proposal` | `{title, body}` for the hub's agent to weigh |

The hub's agent folds it into the [rebuild script](rebuild.md), acts on each `catalog` item, keeps proposals for session end. The heartbeat and `list_apps` still win.

## Sharing between remotes

Remotes never mount each other. Job formats a data disk → hub's agent pools it ([cluster](../storage.md)) → shares a [workspace](../storage.md#a-shared-workspace) → jobs exchange files there. Every export is the hub's.

## Loop, rollback, kill

- Report → done, or a correction on the same remote session. Cap: 3 rounds (Settings), then **needs you**.
- Snapshot before each round (btrfs, LVM); **Roll back** restores. A root job can delete it; ext4 has none → [rebuild script](rebuild.md).
- **Kill** stops the unit now; state `killed`.

## Older remotes

**Re-provision** reruns the layout (accounts, key, hook, pinned agent) and removes the old cage and `homedash-sudo`; also how a pinned `omp` arrives.
