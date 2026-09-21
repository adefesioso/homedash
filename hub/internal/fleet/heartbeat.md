# The heartbeat and the scan

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
