# internal/fleet

The hub's side of the machines it owns: enrollment, the heartbeat that
settles whether each one is really there, the numbers kept per sweep, the
lock, the job door, and the one gated `Run` every command to a remote
goes through.

## Enrollment

`NewCode` mints a single-use code (fifteen minutes, one use) and the line
to paste: `curl -fsSL http://<hub LAN addr>:7434/enroll/<code> | sudo sh`.
`Script` renders `enroll.sh` for a live code: the **layout** (`layout.sh`,
idempotent, everything below) and then a report — name, SSH port, host
key and facts POSTed back. `Report` records the host with the address the
report came from as the pinned address, and `finish` does the rest over
the connection the hub can now open: mint and deliver the host's vault
token, pull the first credential snapshot, run the rebuild script if the
code named a host to rebuild from, and settle the card with a real sweep.
`Reprovision` pipes the same layout over SSH as root to a host already
enrolled — no code, no new record — and then delivers credentials again;
it is how a remote from before this layout, or behind a pinned version,
catches up.

## What enrollment leaves on a remote

Two accounts. `homedash` is the hub's hands: passwordless sudo, the
docker group, and the hub's key — in `/etc/ssh/authorized_keys.d/homedash`,
root-owned and immutable, named by an sshd drop-in, so the hub's access
depends on nothing under a writable home. `homedash-agent` is the job's
account: no sudo, the `homedash` group (for shared storage) and the
`docker` group (so a job runs containers on its own box — root by
another name, held by the hook, not the kernel; see
[safety](../../../docs/running/safety.md)), and the agent's state:

```
/usr/local/bin/{omp,llmfit,homedash-secret,homedash-sudo}   root-owned
/etc/homedash/agent-cage.nft + homedash-agent-cage.service  the network cage, on at boot
/home/homedash-agent/.omp/
  auth-broker.token                       this host's own vault token, delivered over SSH
  agent/config.yml                        modelRoles.default: the fleet default, or the card's override
  agent/models.yml                        the hub's router as provider "homedash", via the job door
  agent/hooks/pre/homedash-refusals.ts    the refused-command list, as an omp hook; root-owned
  cache/auth-broker-snapshot.enc          omp's encrypted credential snapshot
  agent/sessions/                         the remote's own job sessions
```

The cage is one nftables table on the agent's uid: loopback and public
addresses pass, private ranges (the house, the hub, the other remotes)
are rejected. omp is a Bun binary needing roughly a gigabyte to run; a
remote under about 1.5 GB enrolls and pools but the agent is unusable
there, and the card says so.

## The heartbeat

One goroutine, once a minute, every host at once (eight in flight at a
time), each with its own 45-second timeout, so an unplugged machine
costs its own 45 seconds and nobody else's: connect with the pinned key
— the executor keeps that connection open for the next sweep — pipe `facts.sh` over stdin, parse
the JSON it prints — hostname, cores, memory, load, every mount with its
source, disks, what the system and swap sit on (`systemSources`, and
`systemDevices`: those plus the whole chain beneath each, LV, partition,
disk — what the [gate](../gate/README.md) refuses disk tools on), GPU, Docker, Ollama, the
lock as sshd would actually apply it, the agent's version, model and
snapshot age, whether the agent account and the cage exist, and the
network: each interface's IPv4 addresses with whether the kernel marks
them `dynamic` (a lease) or not (held), the default gateway and the
resolver's DNS, and which manager owns the config. A refused or timed-out connection is `offline`; a host key
that doesn't match is `mismatch`. A status change is an event; a repeat
is not. Each sweep records a `metrics` row and checks every mountpoint
against `notify.disk_percent` (90), recording the crossing in either
direction once. `RollupMetrics` averages raw rows into `metrics_hourly`
hourly — only the hours not yet rolled — and drops raw rows
older than two days.

## The scan

A second goroutine, every `network.scan_minutes` (10; 0 off), pipes
`scan.sh` to every online host (eight at a time, a two-minute timeout
each) and merges the
JSON it prints: the host's own interfaces (name, MAC, addresses), its
LAN neighbours (MAC, IP, interface, hostname from mDNS/DHCP/reverse DNS,
mDNS service types, SSDP search targets and server string) after an
`fping`/`ping` sweep of each /24 it sits on, a Wi-Fi scan through `nmcli`
or `sudo iw`, and eight seconds of `bluetoothctl` discovery. Each slice
is `null` when the machine has no interface or tool for it, and
`host_scans` keeps which slices each host could see last time, or why
its scan failed. The script probes nothing but ICMP echo and multicast
queries.

`Merge` is one transaction per scanned host: it upserts one `devices`
row per (kind, address) — enrolled hosts' own MACs are matched to the
host and skipped — and one `sightings` row per device per host, then `identify` recomputes the guess from every
current sighting: hostname, vendor from `oui.go`'s short prefix table,
and a kind from mDNS service types, SSDP device type, Bluetooth class
and finally vendor. A device's first sighting is a `device.new` event.
`Forget` drops unnamed devices unseen for `network.forget_days` (30).

## The forwards and the job door

Every `omp` the hub runs over SSH — a job round, a credential pull, a
command run "with forwards" — gets two reverse forwards on the remote's
loopback for the life of that connection: `8765` to a **vault proxy** for
that host and `11435` to that connection's **job door**. The proxy is a
loopback listener on the hub that accepts only this host's own bearer
token (`hosts.vault_token`, minted at every credential delivery, emptied
by **Revoke**) and swaps in the real one before passing the request to
the vault. The door is a loopback `http.Server` on the hub serving exactly
three things: the router (Ollama's paths, so `homedash/<model>` works),
`GET /secrets/{name}` for that host's grants, and `POST /sudo` — the
command in the body, checked by `gate.Check` and `gate.Privileged`, run
as root through `sudo -n sh -c` on the same connection, its merged
output returned with the exit code in a header, and every request
recorded as a `sudo.run` or `sudo.refused` event and as a job event when
the connection is a job's. `jobs.sudo` off answers every `/sudo` with
403. The panel's listener is never forwarded, so a remote's agent has
no route to the hub's API. `homedash-secret NAME` and `homedash-sudo …`
are curls of the door. Before each omp run the provider cache is removed
and `omp models` rebuilds it through the door, because omp resolves
`--model` from that cache and never refreshes it when the pool gains a
model.

## Held connections

`Hold` keeps one SSH client per host for traffic that would otherwise
dial on every use — a published service's connections — redialing when
the kept one has died and dropping it when the host is removed. `Port`
opens a `direct-tcpip` channel on that client to a port on the remote's
loopback: this is a service's origin side.

## The lock, the address, the model, what fits

`SetLock` writes an sshd drop-in (`PasswordAuthentication no`,
`PermitRootLogin no`, `AllowUsers homedash`) as a candidate, has `sshd -T`
say what it would allow for the hub's account and for `nobody`, and only
then moves it into place and reloads; either check failing removes it.
`SetAddress` holds an interface at a static address, or hands it back
to DHCP. It pipes `address.sh` as root: the script picks the manager
that owns the machine's network (NetworkManager if it is up and has the
interface; else netplan if installed; else systemd-networkd if up; else
ifupdown), writes the config under HomeDash's name for that manager —
`90-homedash-IFACE.yaml`, `00-homedash-IFACE.network`,
`interfaces.d/homedash-IFACE` with the interface's other stanzas
commented out and conflicting drop-ins moved aside, or `nmcli con mod`
on the interface's connection — disables cloud-init's network rendering
where cloud-init exists, and starts a **guard** as a transient systemd
unit that applies the config and then waits ninety seconds for
`/run/homedash-net-confirm`, reverting to the saved config if it never
appears. The hub dials the new address with the pinned host key until
it answers, touches the confirm file, re-pins `hosts.addr`, drops the
held connection, and sweeps; a mismatch or a timeout is an error and
the machine reverts on its own. A second change while the guard is
still up is refused by the script.
`SetAgentModel` writes the card's override into the remote's
`config.yml`; blank means the fleet's remote model (`RemoteModel`:
`agent.remote_model`, or `agent.default_model` when that is blank),
which is also what enrollment's layout writes and a job runs. `Fit` runs `llmfit recommend --json -n 400` and keeps the
entries with an Ollama name, best first; nothing is stored.

## Tables

`hosts` — name, address and port, the account, the pinned host key,
status (`unknown`, `online`, `offline`, `mismatch`), the last facts, the
agent model override, `rebuild_script`, this host's `vault_token`,
enrolled and last seen.
`enroll_codes` — code, name, the host to rebuild from, expiry, used.
`metrics` and `metrics_hourly` — per host per sweep and per hour: memory
used and total, load, GPU busy, free bytes per mountpoint.
`devices` — kind (`lan`, `wifi`, `bt`), address, the guess (name, vendor,
kind), your name and kind, first and last seen.
`sightings` — device, host, last seen, the detail JSON that sighting
carried (IP, interface, hostname, services, SSID, signal).
`host_scans` — per host: when it last scanned, which of LAN, Wi-Fi and
Bluetooth it could see, and the error if the scan failed.

## Secrets

`secrets` — name, ciphertext and nonce (AES-256-GCM under `secrets.key`
in the state dir, 0600, made on first use), `hosts` (comma-separated ids,
empty for every host). Written from Settings; values are never read back
by the panel or a tool. A read through the job door is checked against
the grant and recorded as a `secret.read` event naming the host and the
secret, never the value.

## The rebuild script

`hosts.rebuild_script` is one text column, empty at enrollment. Two
writers: the hub's agent through `rebuild_script_set` after a job's
report, replacing the whole script; and the hub itself, appending a
block for each thing the panel did to that remote — a stack deployed,
Ollama installed or removed, a model pulled, a cluster export or mount, a
workspace share, the agent's model — since it knows the exact command.
It is never run against an enrolled host; enrollment with a host to
rebuild from runs it once, on the new machine, which starts with a copy.
