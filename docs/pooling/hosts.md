# Hosts

## Joining a machine

*Adding a machine is a ritual you half-remember — create a user, copy a
key, pin an address, install Docker, hope. Do it twice and you'll do it
differently twice.*

Press **New remote** and paste one line into that machine's terminal. The
line carries a single-use code and fetches the script from the hub's
**enrollment port** — `:7434`, its own listener apart from the panel and
the API, which answers nothing but a live code's script and that
machine's report, so the one unauthenticated thing the hub serves is
kept off the port everything else signs in to. The script creates the
hub's account on the machine, authorizes the hub's key, pins the
address, installs Docker, installs the [agent](agents/README.md) and
llmfit, and reports the machine's specs and host key back. Over the connection the
hub can now open it delivers the vault token, pulls the first credential
snapshot and, if you named a host to rebuild from, runs its
[rebuild script](agents/rebuild.md) — the panel and `homedash rebuild` edit
the same text, so a save from either is byte-exact.

Both directions of trust are established inside that one window: the
remote learns which key may log in, the hub learns which host key to
expect. A box that was rebuilt or replaced is refused later, not trusted.

A Pi-class box — under about a gigabyte and a half of memory — cannot
run the coding agent at all; it still enrolls, still pools, and the card
says the agent is not usable there.

**There is no HomeDash agent on a remote.** What enrollment installs is
other people's software with installers of its own — Docker, the coding
agent, Ollama if you ask for it — and each has a card action to update or
remove it, so nothing on a remote is a version the hub has to roll out
across a fleet. The hub's own channel is SSH, and the only HomeDash code
on a remote is a fact-collection script the heartbeat pipes in fresh every
time, two three-line helpers the agent uses, and the agent's
[cage](../running/safety.md) — one firewall rule loaded at boot by a
unit that runs once and exits. Nothing of HomeDash's stays running or
listens on a remote.

## Knowing which machines are really there

*"It enrolled once" is not "it's there now". A machine unplugged since
last week still looks joined.*

Enrolling proves a machine could reach the hub, not that the hub can reach
it. So a joined host starts *unknown*, and a background heartbeat settles
it with a real SSH connection and freshly re-read specs. The card says
Online, Offline or Key mismatch on that basis. Events record the
*transition* into and out of trouble, so a machine that's been off for a
week doesn't bury the list.

## Watching it change

*A heartbeat that forgets each reading immediately answers "is that disk
filling, or has it always looked like that?" no better than SSH did.*

Each sweep keeps a handful of numbers — free space per mountpoint, memory
in use, load, whether the GPU was busy — and rolls them up hourly. The same
sweep re-reads what the remote's agent says about itself: its version, the
model it is set to, how old its credential snapshot is. Cards carry a
sparkline next to the number, and the Storage tab draws the same line per
mountpoint, so a disk that will be full on Thursday says so before
Thursday. [Placement](apps.md#where-a-new-app-should-go) reads the trend
as well as the instant.

## Shutting the front door

*Every machine has its own SSH config, so "who can log into that box" is
a question you can't answer.*

The switch on each card turns direct SSH access on or off. Locked, the
machine answers only to the hub's account — password logins, root logins
and every other account refused.

Locking never applies on trust. The lock routine verifies what the
candidate config would actually allow, for the hub's account and for
everyone else, before it takes effect; either check failing leaves the
machine unchanged. The switch only appears while the hub can reach the
machine — a disabled switch beats a promise — and it shows what the
machine last reported, so a lock undone at the console is picked up by
the next heartbeat.

## Holding an address

*The hub pins the address a machine enrolled from, and a router hands
out a different one after a power cut. The machine is fine; the hub
calls it offline. Fixing it means remembering which of four network
stacks that box runs and where each keeps its config.*

**Address** under a card's **More** shows each interface as the machine
reports it — address, and whether it came from DHCP or is held — with
the gateway and DNS the machine is using. Pick an interface, keep or
change the address, and **Hold this address**; **Back to DHCP** undoes
it. The hub writes the config in whatever the machine actually manages
its network with — NetworkManager, netplan, systemd-networkd or
ifupdown — under HomeDash's own name, and tells cloud-init to leave
networking alone on a box it set up, so the setting survives a reboot.

A wrong address is a machine you can no longer reach, so the change is
never trusted on the machine's say-so. The machine applies it under a
guard and starts a ninety-second clock; the hub then has to reach it at
the new address with the pinned host key and confirm. Confirmed, the
hub re-pins the address and the card follows. Not confirmed — the hub
could not reach it, or a different host key answered there — the
machine puts its previous config back on its own, and the card says the
change was refused. Back to DHCP is confirmed the same way, at the
address the machine had: a lease that hands out a different one is not
confirmed, and the machine keeps its held address — set the one you
want instead.

## Publishing a port

Any port on a host card can be **published** as a service reachable from
outside the house through the peers you name — see
[publishing a service](../sharing/services.md). Nothing changes on the
machine.
