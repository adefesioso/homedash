# internal/gate

The list: what would lock the house out, refused in code for every caller (panel, CLI, assistant, task). The structural half (every operation names a host; the hub isn't one) is the [store's](../store/README.md) host resolver. A root job doesn't pass here; what holds it is in [safety](../../../docs/running/safety.md).

| Function | Refuses |
| --- | --- |
| `Check(command)` | Firewall off, hub SSH cut, SSH config edits, system disk, system path, reboot, fork bomb — [rules.md](rules.md) |
| `CheckOn(command, system)` | Same, with the machine's system devices; no machine → every disk is the system disk |
| `ComposeRefusal(compose)` | The docker danger list, read from a compose file an agent deploys |

- A refusal is a `Refusal` with one reason; callers relay it and record `gate.refused`. `Reasons` is the panel's list.
- [The hook](rules.md#the-hook): the same rules plus the hold and fleet addresses, for a root job's own tools. Keep in step with Go; `gate_test.go` covers Go.
