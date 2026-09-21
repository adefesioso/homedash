# Network

*A house has far more on its network than the machines you enrolled — a
television, a printer, a fridge that wants a firmware update, phones,
speakers, a Pi you forgot behind the router — and nothing lists them.
Each enrolled machine sees a different slice of it: the desktop has the
Wi-Fi card, the Pi has the Bluetooth radio, the NAS on a cable sees the
wired neighbours. The hub, in a closet on a cable, sees the least of all.*

The **Network tab** is every device the remotes can see, in one list,
with the hub's best guess at what each one is and your word on top of it.
It is there for you to read, and there so the hub's agent knows which
remote can reach the television when you ask it to do something to the
television.

## The concepts

- **A device** is anything a remote can see that is neither the hub nor
  an enrolled remote: a neighbour on the wired or wireless LAN, a Wi-Fi
  network in range, a Bluetooth device in range. Its identity is its
  hardware address — MAC for a neighbour or a Bluetooth device, BSSID for
  a Wi-Fi network — so the same television seen from three remotes is
  one device seen three ways.
- **A sighting** is one remote seeing one device at one time, with what it
  saw: address, hostname, signal, the services it advertised. The tab
  shows what remotes last saw, never what the hub assumed, and says which
  remotes saw it and when.
- **The scan** is the hub asking each online remote what it can see, on
  an interval you set, the same way the heartbeat asks for facts: a
  script piped in fresh, nothing installed.
- **The guess** is the hub's identification of a device from the
  evidence: a name, a vendor, and a kind. **Your word** — a name and a
  kind you set on the device — sits beside the guess, is never
  overwritten by it, and outlives the device being forgotten.

## What it does

### Seeing what's there

*Every remote sees something different, and no single one sees the house.*

Every `network.scan_minutes` (ten by default; zero switches scanning
off), the hub pipes a scan script to each online remote and merges what
comes back. The script reads only what the machine already knows or can
hear: the kernel's neighbour table after a one-shot ping of its own /24
subnets, what mDNS and SSDP devices announce about themselves, a Wi-Fi
scan on any wireless interface, a few seconds of Bluetooth discovery.
It never opens a port on a device and never connects to one. A remote
without a Wi-Fi card, a Bluetooth radio or the tools for them simply
reports that slice as absent, and the tab shows per remote which of
**LAN**, **Wi-Fi** and **Bluetooth** it can see, so "nothing here can
see Bluetooth" is a thing the tab can say.

Scanning is a sweep on the remotes, not a service on them, so a remote
that is offline is a remote not scanning — its row keeps its last scan
but is marked offline rather than read as current — and a device nobody
has seen
for `network.forget_days` (thirty by default) leaves the list — unless
you named it, in which case it stays with its last sighting and is
marked missing. The first sighting of a device is an event; a repeat is
not.

### Naming what it sees

*A row that says `a4:83:e7:…` is a row you scroll past.*

Each device carries the hub's guess, built from the evidence the remotes
brought back: a hostname from mDNS, DHCP or reverse DNS; a vendor from
the hardware address's registered prefix, against a short table of the
vendors a house tends to hold; a kind — television, speaker, printer,
phone, computer, camera, light, appliance, router — from what the device
advertised (a Chromecast or AirPlay receiver is a television or speaker,
an IPP printer is a printer, a HomeKit accessory is what it says it is)
and from the vendor when nothing else says. The guess is labelled as a
guess. What the scan can't tell is left unknown rather than invented.

A remote's own hardware addresses are known from its facts, so a remote
seen by another remote is shown as that remote, not as a stranger.

You can give any device a name and a kind. That is the only thing the
hub keeps about a device that a remote didn't report, and the tab shows
both: what you called it, and what the scan would have called it.

### Letting the agent use it

*"Turn the TV off at midnight" is only hard because nothing knows which
machine can reach the TV.*

The hub's agent gets `list_devices`, and the
[CLI](../running/cli.md) `homedash devices`: every device with its addresses, its guess, your
name for it, and which remotes last saw it from which interface. That is
what lets a request like "have something check the fridge's firmware
every Sunday" become a [job](agents/jobs.md#a-job) or a
[task](tasks.md) on the remote that can actually see the fridge, using
whatever that remote has — `curl`, a vendor CLI, Bluetooth tools — on
that machine and nowhere else. `name_device` lets the agent record what
it worked out, the same way you would.

The hub itself never touches a device. Every interaction with one is a
command on a remote, on your instruction, through the same
[gate](../running/safety.md) as any other. Devices are never offered to a
peer: a [space](../sharing/README.md) sees inference and published
services, and nothing about what is on your LAN.
