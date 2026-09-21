# Passkeys and the WebAuthn RP ID

Passkeys are always registered for `localhost`, which is why the launcher
opens `http://localhost:7433` rather than the loopback address. WebAuthn
requires the RP ID to be a real domain — never a bare IP — so a hub
reached only by IP can't do passkeys there no matter what; the hub
refuses to start rather than silently fail sign-in if you set `auth.rpid`
to one.

If `HOMEDASH_ADDR` binds directly to a LAN *hostname*, or `hub.lan_addr`
in Settings holds one (e.g. `homedash.local` over mDNS/Avahi), that name
is registered too — but browsers offer WebAuthn over plain `http://` only
on `localhost`, so any other name needs HTTPS in front, with `auth.rpid`
set to the name and `auth.origins` to its `https://` origin in Settings
(both read at start). That is what
[`packaging/https.sh`](../../packaging/https.sh) sets up — see
[opening the panel from other machines](../../../docs/running/https.md).
