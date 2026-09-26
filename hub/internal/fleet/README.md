# internal/fleet

The hub's side of the machines it owns: enrollment, the heartbeat that
settles whether each one is really there, the numbers kept per sweep, the
lock, the job door, and the one gated `Run` every command to a remote
goes through.

## Enrollment

Mints the enroll code, renders `enroll.sh`, and lays down the hub's
account, the agent's home and hook, and removes an older layout's cage
and sudo door.
[enrollment.md](enrollment.md)

## The heartbeat and the scan

Two goroutines: one settles online/offline/mismatch and per-host metrics
every minute, the other maps each host's LAN/Wi-Fi/Bluetooth neighbours
every ten. [heartbeat.md](heartbeat.md)

## The forwards and the job door

Every SSH connection to a remote carries a vault proxy and a job door —
the local-only router and the secrets a job reaches over loopback — plus
held connections for published services, and the hub's hold checked and
restored around a root job. [job-door.md](job-door.md)

## The lock, the address, the model, what fits

`SetLock` hardens sshd with a rollback check; `SetAddress` moves an
interface to a static address behind a self-reverting guard;
`SetAgentModel` and `Fit` pick and size what a remote runs.
[network.md](network.md)

## Tables

Schema for `hosts`, `enroll_codes`, `metrics*`, `devices`, `sightings`,
`host_scans`. [tables.md](tables.md)

## Secrets

Per-host encrypted values, keyed and read only through the job door.
[tables.md](tables.md#secrets)

## The rebuild script

How a host's rebuild script is written and replayed.
[tables.md](tables.md#the-rebuild-script)

## Mobile

`mobile.go`: pairing an Android phone instead of enrolling it over SSH —
its own code, device key and heartbeat.
[../../../docs/pooling/mobile.md](../../../docs/pooling/mobile.md)
