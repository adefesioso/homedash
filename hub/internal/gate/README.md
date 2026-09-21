# internal/gate

The list. Anything that would lock the house out is refused here in
code, however it is asked for — panel, window, outside assistant, task,
`homedash-sudo`. The structural half of the gate (every operation names
a host, and the hub is not one) lives in the [store's](../store/README.md)
host resolver; the kernel half (a job's account has no privilege) lives in
[enrollment](../fleet/README.md); this is the other half.

Two checks, both in [rules.md](rules.md):

- **`Check(command)`** — refused everywhere, always: the firewall, the
  hub's own SSH access, the system disk, a system path, a reboot, a fork
  bomb. This is what the API, the panel and every tool call go through.
- **`Privileged(command)`** — the second, stricter check for the one
  place a job's account gets root, the `homedash-sudo` door: an allowlist
  of programs and a denylist of protected paths, on top of `Check`.

**The system disk, and only the system disk** is refused — a data disk on
a remote is ordinary work once it has a filesystem. `CheckOn`/`PrivilegedOn`
take a machine's reported system devices; `Check`/`Privileged` with no
machine in hand treat every disk as the system disk. See
[rules.md](rules.md#the-system-disk-and-only-the-system-disk).

A refusal is a `Refusal` error with the one reason; callers relay it and
record a `gate.refused` event. `Reasons` is the list the panel shows.

[The hook](rules.md#the-hook) is the same rules as an omp pre-tool hook,
written root-owned to the agent account by enrollment, so a remote's
agent meets the list on its own tools too, plus one extra rule the API
doesn't need (a privileged Docker container). Keep it in step with the
Go side — `gate_test.go` covers the Go side.
