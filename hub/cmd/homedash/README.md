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
things while a single-use code is live — `GET /enroll/<code>`, the
script, and `POST /enroll/<code>`, its report — and 404s everything else.
The panel's line uses the hub's LAN address, from its default route
unless `hub.lan_addr` in Settings overrides it.

Passkeys need a real domain as the WebAuthn RP ID, never a bare IP —
see [rpid.md](rpid.md) for `auth.rpid`/`auth.origins` and reaching the
panel by LAN hostname or HTTPS. On every start the hub also mints a
fresh admin token named `hub-agent` for its own omp windows, so they are
a client like any other.

## `homedash` (the launcher)

With no subcommand, the desktop launcher: waits up to five seconds for
the local hub to answer `/api/health`, then opens the panel as a
detached app-style browser window (`--app=` on Chromium, its own window
on Firefox, `xdg-open` as fallback) — no embedded web view, since
passkeys need a real browser and don't reliably work in WebKitGTK.

## The shell's authority

Run as the hub's user (`sudo -u homedash homedash …`), each records an
event:

- `recover` prints a one-time admin registration code.
- `invite [admin|viewer]` prints a code for a new account (viewer by
  default).
- `token NAME` prints an admin API token, once.
- `export FILE` writes the state directory as one file encrypted under a
  passphrase (terminal, or `HOMEDASH_PASSPHRASE`); the hub may be running.
- `restore FILE` puts an export back; refuses while the hub is running.
- `version` prints the build's version.

## The CLI

Any other subcommand is the [CLI](../../internal/cli/README.md): the
same binary on a workstation, talking to a hub over its API with the
session `homedash login <hub-url>` left in `~/.config/homedash/`.
`serve`, `open` and the three shell subcommands above are the hub's own;
everything else — `hosts`, `run`, `start`, `deploy` … — is the fleet, as
[the docs](../../../docs/running/cli.md) list it.
