# The forwards and the job door

Every `omp` run over SSH (job round, credential pull, command "with forwards") gets two reverse forwards on the remote's loopback, for the connection's life:

| Port | To | Serves |
| --- | --- | --- |
| `8765` | Vault proxy | Accepts only this host's `hosts.vault_token` (minted per delivery, emptied by **Revoke**), swaps in the real one |
| `11435` | Job door | Router (Ollama paths) marked `X-HomeDash-Local-Only` — no peer answers a root agent; `GET /secrets/{name}` for this host's grants |

- The panel's listener is never forwarded: no route to the hub's API.
- `homedash-secret NAME` curls the door.
- Before each omp run the provider cache is deleted and `omp models` rebuilds it through the door (omp never refreshes it when the pool gains a model).

## Held connections

`Hold`: one SSH client per host for published services; redialed when dead, dropped on removal. `Port`: a `direct-tcpip` channel to the remote's loopback — the service's origin side.

## The hold, around a root job

- `ArmJob` (before every round, root): writes the hook (`gate.Hook`) and `/etc/homedash/fleet-addrs` (`hub.lan_addr`, the enroll URL's host, every other host's address).
- `hold.sh`: rewrites the hub key file, the sshd drop-in, the `homedash` sudoers line if they differ; recreates the account (with `docker`); enables, starts, reloads sshd.
- Run 1: the unit's `ExecStopPost` (`HoldStopPost`) — inline base64, beyond a job's edits and systemd's `$`/`%` expansion; runs on done, timeout, kill; needs no sudo; result → `/etc/homedash/hold-restored`.
- Run 2: `CheckHold` over the round's connection; prints that file plus its own result.
- Restored → `hold` job event + `host.hold_repaired`. Unfixable (sshd rejects its config, `AllowUsers` omits `homedash`) → `host.hold_broken`.
