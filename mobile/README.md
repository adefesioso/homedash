# mobile

*[Mobile](../docs/pooling/mobile.md) gave the hub a pairing code, a
device key and a status-push endpoint for a phone to speak — nothing on
the phone's side to speak it with.*

This is that side: a single-Activity Kotlin app that pairs once, then
runs a foreground service reporting battery, storage, screen, network
and the foreground app once a minute for as long as its notification is
showing.

## Pairing

Type the hub address (host:port from **Add mobile** on the Hosts tab, no
scheme) and the code into the two fields. The app posts both, plus a
first snapshot of its own stats, to `/enroll/{code}` and stores the
device key the hub hands back — see
[Pairing, not enrolling](../docs/pooling/mobile.md#pairing-not-enrolling)
for the wire shape.

## Reporting

`StatusService` is a foreground service, not `WorkManager`: the hub
calls a phone offline after three minutes of silence
([the heartbeat runs backwards](../docs/pooling/mobile.md#the-heartbeat-runs-backwards)),
and `WorkManager`'s periodic minimum is fifteen. It posts every sixty
seconds, and stops itself — clearing the stored device key — the moment
the hub answers 401 or 404, since that only happens once pairing has
been undone on the hub's side.

The foreground app needs the special "usage access" permission, granted
by hand from the paired screen's button; without it, that one field is
just left out of the push.

## Building

`./gradlew assembleDebug` from this directory (needs `ANDROID_HOME` set,
or a `local.properties` with `sdk.dir`), or open in Android Studio.
minSdk 26, compileSdk 34.

Verified against a real hub: a from-scratch `homedash serve` on a
scratch state dir, an admin token minted with `homedash token`, and
`POST /api/hosts/mobile/enroll` for a live code — the app paired,
appeared on `/api/hosts` with `kind":"mobile"` and the right facts
shape, pushed a status update every 60s, and cleared its own pairing
the moment the host was removed on the hub's side.
