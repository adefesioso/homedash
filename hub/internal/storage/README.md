# internal/storage

The pooled disks and the shared workspaces on them. The hub sets mounts
up over SSH and then leaves the data path; every command here runs on
the remotes as root through the executor, and each is appended to the
host's rebuild script.

## Clusters

A cluster is mergerfs over NFS. On each member that is not the gateway,
`/etc/exports.d/homedash-<cluster>-<member>.exports` exports the member's
path to the gateway's address only (`rw,sync,no_subtree_check,
no_root_squash`). On the gateway, each remote member is an NFS branch at
`/mnt/homedash/<cluster>/<host>-<member>` (`soft`, `timeo=30`,
`retrans=2`, `nofail`, `x-systemd.automount`), a member on the gateway
itself is a branch by its own path, and mergerfs merges the branches at
the cluster path (`category.create=mfs`, `moveonenospc`,
`minfreespace=1G`). All of it is in `/etc/fstab` tagged
`homedash-cluster-<name>`, so a reboot brings it back and `apply` — the
whole mount from the current member list, idempotent — rewrites exactly
those lines.

`refuse` is the gate for members: a path must be a mountpoint the
machine reported, not `/`, `/boot`, `/etc`, `/usr`, `/var`, `/home` or
`/root`, not on a source the facts list as carrying the system or swap,
and not a cluster's own mount. `RemoveMember` rebuilds without the member
and drops its export; `Delete` unmounts everything and drops every
export; files stay where they are in both cases. `Statuses` asks each
gateway, live, for the merged mount's size and free space and whether
each branch answers `stat` within five seconds; a branch that doesn't is
**Degraded**.

## Workspaces

A workspace is a directory `<cluster path>/<name>` on the gateway,
shared to the named remotes so it appears at that same absolute path on
each. On the gateway, `/etc/exports.d/homedash-ws-<name>.exports` exports
it to exactly the named remotes' addresses, `all_squash` to the gateway's
`homedash` uid so every remote's writes land as one owner, with a stable
`fsid` (mergerfs is FUSE and needs one to be exported). On each named
remote that is not the gateway, an fstab line tagged
`homedash-workspace-<name>` mounts it at the same path with the cluster's
own soft-mount options. Whether a remote actually has it mounted is read
from the mounts that remote's heartbeat reports, never assumed.

`AddWorkspaceMember` and `RemoveWorkspaceMember` rewrite the export line
and mount or unmount on that one remote; `DeleteWorkspace` unshares from
every remote and leaves the directory and its files on the cluster. A
job names the path as its working directory and needs nothing else.

## Tables

`clusters` — name, gateway, path. `cluster_members` — host + path,
unique. `workspaces` — name, cluster. `workspace_members` — workspace +
host, unique.
