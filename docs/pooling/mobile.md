# Mobile

*A phone has no root shell to pipe `enroll.sh` into, and nothing on it is
memory, load or a mount — [Hosts](hosts.md)'s whole enrollment and card
shape assumes a box you SSH into.*

## Pairing, not enrolling

**Add mobile** on the Hosts tab mints a pairing code — good once, for
fifteen minutes, like a compute remote's line, but typed into the
companion app instead of a terminal. The app posts to the hub's
enrollment listener (`:7434`, the same unauthenticated door
[Hosts](hosts.md#joining-a-machine) uses) to spend the code, and gets
back a device key: a bearer secret the hub mints, not one the phone
supplies, since there is no host key to pin here — the phone calls in,
the hub never dials out to it.

That key is the phone's whole credential. Every status push after
pairing carries it as `Authorization: Bearer`; a wrong or missing key is
refused. There is no lock, no SSH, no rebuild script — nothing to
secure beyond that one shared secret.

## The heartbeat runs backwards

A compute remote is *dialed*: the hub's heartbeat connects over SSH once
a minute and settles online/offline from whether that connection works.
A phone is never dialed — the hub has no route to it — so it *reports
in* on its own instead, posting whatever it can currently read to
`POST /mobile/{host}/status`. Online is a status push landing; offline
is the same heartbeat noticing three minutes have passed without one.

## What the card shows

Stats only, for now: battery (and whether it's charging), storage free
of total, screen state, network, and the foreground app, whatever subset
the app last reported. Puppeteering — the app acting on the hub's
instructions rather than only reporting — is not built yet; the card
reads what the phone says, the way an early compute card did before the
agent gave it something to run.

## What the card doesn't show

Everything on a [Hosts](hosts.md) card that assumes SSH: the lock
switch, Ollama, credentials, re-provision, publish a port, the rebuild
script, address holding. None of it applies to a phone the hub never
dials into, so a mobile card's **More** is just Remove.

## The app

[mobile/](../../mobile/README.md) is the companion app: a pairing screen
that spends the code, then a foreground service that reads battery,
storage, screen and the foreground app once a minute and posts them to
`/mobile/{host}/status` with the device key it was handed. Nothing on
the hub side changes to add it — it is just the first thing that speaks
this doc's protocol.
