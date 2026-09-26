# The refusal rules

## Check

Splits at `;` `|` `&&` `||` newline; strips wrappers (`sudo`, `env`, `nohup`, …) and assignments; asks every rule per program. A redirect into `/etc/ssh/` is an edit.

| Refusal | What matches |
| --- | --- |
| switching off the firewall | `ufw disable/reset`, `iptables -F`, `nft flush ruleset`, stopping ufw/firewalld/nftables |
| cutting the hub's own SSH access | stopping sshd, removing `/etc/ssh/*` or `authorized_keys`, deleting or locking the `homedash` accounts |
| editing SSH config by hand | `sed`/`perl`/`tee`/… naming `/etc/ssh/` |
| repartitioning or wiping the system disk | a disk tool (`fdisk`, `sfdisk`, `parted`, `gdisk`, `wipefs`, `mkfs*`, `mkswap`, `dd of=`, `tune2fs`, `resize2fs`, `mdadm`, `pvcreate`/`vgcreate`/`lvcreate`/`*remove`, `cryptsetup`) or a redirect naming a device the system or swap sits on — the source, the partition, the whole disk beneath — or a device named by a symlink (`/dev/disk/by-*`, `/dev/vg/lv`) the hub cannot resolve, or any device at all when the machine has not reported which disk carries its system |
| deleting a system path | a recursive `rm` naming `/`, `/etc`, `/usr`, `/home`, … |
| shutting down or rebooting | `shutdown`, `reboot`, `poweroff`, `init 0/6`, `systemctl reboot` |
| a fork bomb | the classic `:(){ :|:& };:` |

## The system disk, and only the system disk

- A data disk is ordinary work; only the disk the machine runs from is refused.
- `systemDevices` (facts): sources of `/`, `/boot`, `/var`, `/home`, swap, plus LV → partition → disk beneath.
- Compared by kernel name (`sdb`, `nvme0n1p2`, `vg-root`); a system disk's partition is refused with it.
- `/etc/fstab` is not on the list.

## The hook

`hook.ts` = `Check`'s rules as an omp pre-tool hook, plus the root-job limits. Written to the agent home's `.omp/agent/hooks/pre/` at enrollment and every round.

| Refusal | What matches |
| --- | --- |
| cutting the hub's own SSH access | the `Check` row, plus `rm`/`mv`/`cp`/`install`/`chattr`/`chmod`/`chown` or a redirect naming the hold: `/etc/ssh`, `authorized_keys`, `/etc/sudoers*`, `/etc/pam.d`, `/etc/homedash`, the hub account's home, the helpers, `omp`, the hook |
| the hold, other tools | a write or edit whose `path` names the hold (reads pass) |
| reaching the hub or another remote | a command naming an address in `/etc/homedash/fleet-addrs` |

- System devices: the `findmnt` + `lsblk -s` walk at load; failure → every disk refused.
- A root job can bypass it: stated rule + log line; the controls are in [safety](../../../docs/running/safety.md).
