# internal/gate

The list. Anything that would lock the house out is refused here in
code, however it is asked for — panel, window, outside assistant, task,
`homedash-sudo`. The structural half of the gate (every operation names
a host, and the hub is not one) lives in the [store's](../store/README.md)
host resolver; the kernel half (a job's account has no privilege) lives in
[enrollment](../fleet/README.md); this is the other half.

`Check(command)` splits a command line into simple commands at `;`, `|`,
`&&`, `||` and newlines, strips wrappers (`sudo`, `env`, `nohup`, …) and
environment assignments, and asks every rule about each program and its
arguments. A redirect into `/etc/ssh/` counts as an edit. The rules:

| Refusal | What matches |
| --- | --- |
| switching off the firewall | `ufw disable/reset`, `iptables -F`, `nft flush ruleset`, `nft delete/flush table … homedash-agent`, stopping ufw/firewalld/nftables/homedash-agent-cage |
| cutting the hub's own SSH access | stopping sshd, removing `/etc/ssh/*` or `authorized_keys`, deleting or locking the `homedash` accounts |
| editing SSH config by hand | `sed`/`perl`/`tee`/… naming `/etc/ssh/` |
| repartitioning or wiping the system disk | a disk tool (`fdisk`, `sfdisk`, `parted`, `gdisk`, `wipefs`, `mkfs*`, `mkswap`, `dd of=`, `tune2fs`, `resize2fs`, `mdadm`, `pvcreate`/`vgcreate`/`lvcreate`/`*remove`, `cryptsetup`) or a redirect naming a device the system or swap sits on — the source, the partition, the whole disk beneath — or a device named by a symlink (`/dev/disk/by-*`, `/dev/vg/lv`) the hub cannot resolve, or any device at all when the machine has not reported which disk carries its system |
| deleting a system path | a recursive `rm` naming `/`, `/etc`, `/usr`, `/home`, … |
| shutting down or rebooting | `shutdown`, `reboot`, `poweroff`, `init 0/6`, `systemctl reboot` |
| a fork bomb | the classic `:(){ :|:& };:` |

The hook on a remote adds one row the API's `Check` does not need,
because there the agent's account is in the docker group and reaches the
daemon without the door: `docker` with `--privileged`, a host namespace,
`--cap-add`, `--security-opt`, or a bind of `/`, the docker socket or a
protected path is "a privileged container" — the same shape `Privileged`
refuses below.

**The system disk, and only the system disk.** Formatting, partitioning
and mounting a *data* disk on a remote is ordinary work — a raw disk is
pooled only once someone has put a filesystem on it — so the disk rule
refuses the disk the machine runs from and nothing else. `CheckOn(command,
system)` and `PrivilegedOn(command, system)` take that machine's
`systemDevices` (from its facts: every source `/`, `/boot`, `/var`,
`/home` and swap sit on, plus the whole chain beneath each, LV → partition
→ disk); `Check` and `Privileged` are the same with no machine in hand,
where every disk counts as the system disk. A device compares by kernel
name (`sdb`, `nvme0n1p2`, `mapper/vg-root` by its `vg-root`), and a
partition of a system disk is refused with it. Writing `/etc/fstab` was
never on the list: `tee`, `sed`, `mount -a` and `systemctl daemon-reload`
go through the door, and the hub's own storage code edits it the same way.

A refusal is a `Refusal` error with the one reason; callers relay it and
record a `gate.refused` event. `Reasons` is the list the panel shows.

`Privileged(command)` is the second check, for the one place a job's
account gets root: the `homedash-sudo` door. After `Check`, every simple
command's program must be on the allowlist — package managers,
`systemctl`, `journalctl`, `docker` (never `--privileged`, a host
namespace, an added capability or a bind of a protected path), `mount`,
`umount`, the disk tools above plus `partprobe`, `swapon`/`swapoff`,
`fsck*`, `e2label`, `xfs_*`, `btrfs`, `udevadm`, and the ordinary file
tools — and no word or redirect may name
a **protected path**: `/etc/ssh`, `/etc/sudoers*`, `/etc/pam.d`, the
account databases, `/etc/homedash`, `/etc/nftables*`, `/root`, the hub
account's home, the helpers and `omp` under `/usr/local/bin`, and the
agent's hook directory. Shells, interpreters, `su`/`sudo`, user and
group tools, `chattr`, `setcap`, `nsenter`, `chroot`, `systemd-run`,
`nft`/`iptables`/`ufw`, `crontab` and setuid modes are refused by name.
`ProtectedPaths` is the list, exported for the hook.

`hook.ts` is the same rules as an omp pre-tool hook. `Hook` is its text,
and enrollment writes it, root-owned, to the agent account's
`~/.omp/agent/hooks/pre/`, so a remote's agent working on its own machine
meets the list on its own tools too — `bash` against the command list,
and every other tool's `path` against the protected paths. The hook
reads the machine's own system devices when it loads (the same `findmnt`
+ `lsblk -s` walk `facts.sh` does); if that fails it refuses every disk,
as `Check` does. Keep the two in step: `gate_test.go` covers the Go side.
