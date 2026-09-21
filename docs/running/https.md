# Opening the panel from other machines

*The launcher opens `http://localhost:7433` on the hub itself. A hub is
the machine nobody sits at, so the panel is really opened from a laptop
or a phone across the house — and there, plain `http://` cannot sign
in.*

## Why the panel needs HTTPS off the hub

Sign-in is [passkeys](accounts.md), and browsers offer WebAuthn only in a
**secure context**: `https://` anywhere, or `http://` on `localhost` and
nowhere else. Opened as `http://homedash.local:7433` from another
machine the panel shows *This browser cannot do passkeys* — the API is
simply absent, whatever the browser or the hub is set to.

So a hub opened from the network sits behind a certificate. Nothing on
the internet has to know the name: a certificate from a CA you make
yourself is trusted once per device and works for years.

## What the script does

[`hub/packaging/https.sh`](../../hub/packaging/https.sh) does that on
the hub, once, as root:

```
sudo ./https.sh                    # https://homedash.local
sudo ./https.sh hub.lan            # any name your network resolves
```

It leaves four things behind:

- **The name `homedash.local`**, whatever the machine is called: Avahi's
  `host-name` in `/etc/avahi/avahi-daemon.conf` is set to `homedash`, so
  the hub answers to `homedash.local` over mDNS from every device in
  the house (and no longer to `<hostname>.local`). A name given on the
  command line that ends in `.local` is published the same way; any
  other name is left to your router's DNS.
- **Caddy**, from Debian, on `:443`, with `/etc/caddy/Caddyfile`
  serving the name over `tls internal`: Caddy makes a local CA, issues
  the certificate from it and renews it on its own. Requests are
  proxied to wherever `homedash.service` listens, with
  `X-Forwarded-Proto` set so the hub's session cookie is `Secure`.
- **The hub's passkey origin**: `auth.rpid` set to the name and
  `auth.origins` to its `https://` origin in Settings, through a
  one-off admin token the script mints and revokes, then a hub restart
  (both are read at start). A passkey is bound to one origin, so from
  then on sign-in is at `https://<name>` and no longer at the
  launcher's `http://localhost:7433`.
- **`~/homedash-ca.crt`**, Caddy's root CA, in the home of whoever ran
  `sudo`, for the next step.

Run it before the first passkey is registered if you can: an account
made at `http://localhost` is bound to that origin and cannot be moved.

## Trusting the CA on your devices

Copy `homedash-ca.crt` off the hub (`scp user@hub:homedash-ca.crt .`)
and import it as a trusted authority once per device — a browser refuses
passkeys on a certificate it does not trust:

- **Linux** — `sudo cp homedash-ca.crt /usr/local/share/ca-certificates/
  && sudo update-ca-certificates`; Chromium also under
  `chrome://settings/certificates › Authorities › Import`, Firefox under
  Settings › Privacy & Security › Certificates › Authorities › Import.
- **macOS** — open it in Keychain Access, set it to *Always Trust*.
- **Windows** — open it, *Install Certificate*, store *Trusted Root
  Certification Authorities*.
- **iOS** — AirDrop or mail it, install the profile under Settings ›
  General › VPN & Device Management, then enable it under Settings ›
  General › About › Certificate Trust Settings.
- **Android** — Settings › Security › Encryption & credentials › Install
  a certificate › CA certificate.

Then open `https://<name>` and register the first passkey.

## Limits

- The hub takes `:443`/`:80` itself when a peer's service is approved
  as a [public ingress](../sharing/services.md); that collides with
  Caddy here. A hub that fronts services publicly is already behind a
  real domain and a real certificate — point that setup's proxy at the
  panel too and set `auth.rpid`/`auth.origins` by hand instead of
  running this.
- The name has to resolve from every device: `homedash.local` does
  over mDNS (Avahi on the hub, installed by the script; Windows needs
  Bonjour), anything else needs your router's DNS.
- Caddy's own CA is a root your devices trust; keep the hub as safe as
  you keep a hub.
