# cmd/homedash

One binary, two doors, the shell's authority, and the CLI.

## `homedash serve`

The hub. It listens on `127.0.0.1:7433` by default (`HOMEDASH_ADDR`),
keeps its state under `/var/lib/homedash` (`HOMEDASH_STATE`) and is what
the systemd unit runs, as the `homedash` system user, restarting on
failure. Start order: the identity key, the store, the agent (omp and the
vault), the notifier, the fleet heartbeat, the pool, peers, the
scheduler, apps, storage, backup, sign-in, then the two listeners. Before
any of that, a restore staged by the panel (`restore.pending/` in the
state directory) is applied, and recorded as an event once the store is
open.

Enrollment needs the remote to reach the hub once, so a second listener,
`:7434` on every interface (`HOMEDASH_ENROLL_ADDR`), answers exactly two
things — `GET /enroll/<code>`, the script, and `POST /enroll/<code>`, its
report — and only while that single-use code is live. Everything else on
that port is a 404. The line the panel shows uses the hub's LAN address,
worked out from its default route; `hub.lan_addr` in Settings overrides
it.

Passkeys are always registered for `localhost`, which is why the launcher
opens `http://localhost:7433` rather than the loopback address. WebAuthn
requires the RP ID to be a real domain — never a bare IP — so a hub
reached only by IP can't do passkeys there no matter what; the hub
refuses to start rather than silently fail sign-in if you set `auth.rpid`
to one. If `HOMEDASH_ADDR` binds directly to a LAN *hostname*, or
`hub.lan_addr` in Settings holds one (e.g. `raspberrypi.local` over
mDNS/Avahi), that name is registered too, so passkeys also work when the
panel is opened straight from a browser on the network instead of the
launcher. `auth.rpid` and `auth.origins` in Settings override all of
this, which is what a reverse proxy behind its own hostname needs. On
every start the hub also mints a fresh admin token named `hub-agent` for
its own omp windows, so they are a client like any other.

## `homedash` (the launcher)

With no subcommand, the desktop launcher: it waits up to five seconds for
the local hub to answer `/api/health`, then opens the panel as an
app-style browser window — Chromium-family browsers with `--app=`,
Firefox in its own window, `xdg-open` as the fallback — fully detached.
Passkeys work in the browser on `localhost`; they do not reliably work in
WebKitGTK, which is why there is no embedded web view.

## The shell's authority

Run as the hub's user (`sudo -u homedash homedash …`), each records an
event:

- `recover` prints a one-time admin registration code.
- `invite [admin|viewer]` prints a code for a new account (viewer by
  default).
- `token NAME` prints an admin API token, once.
- `export FILE` writes the state directory as one file encrypted under a
  passphrase (asked on the terminal, or `HOMEDASH_PASSPHRASE`); the hub
  may be running.
- `restore FILE` puts an export back, with the hub stopped: it refuses
  while the hub answers on its port. The passphrase is asked the same way.
- `version` prints the build's version.

## The CLI

Any other subcommand is the [CLI](../../internal/cli/README.md): the
same binary on a workstation, talking to a hub over its API with the
session `homedash login <hub-url>` left in `~/.config/homedash/`. `serve`,
`open` and the three above are the hub's own; everything else — `hosts`,
`run`, `start`, `deploy` … — is the fleet, as
[the docs](../../../docs/running/cli.md) list it.
