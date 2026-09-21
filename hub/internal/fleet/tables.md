# Tables, secrets, the rebuild script

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
