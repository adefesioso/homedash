# Running it

*The hub has to run whether or not anyone is looking at it — heartbeats,
schedules and peers' jobs all happen while the panel is closed.
And it is the cheapest machine in the house running the longest hours,
so it is the thing most likely to die.*

## One small box

The hub is not where the work happens. Models run on the remotes, apps run
on the remotes, pooled disks move data between remotes, agent jobs run on
the remotes — the hub keeps a few hundred kilobytes of state, holds SSH
connections open, copies bytes from one socket to another, and runs the
credential vault and one `omp` process per open session. A session costs
the hub about what a browser tab costs, and a Pi-class hub holds a few of
them. So the floor is set low on purpose: a retired laptop, a NAS that runs
containers, a mini PC and a Raspberry Pi are all the same machine to
HomeDash. The hub is the one machine in the fleet that never runs a
model; a card in it is a card wasted, and belongs
[enrolled as a remote](../pooling/hosts.md) instead.

**Spending more buys you more open sessions, a deeper queue and more
concurrent streams, and nothing else** — no amount of hub is going to make
a token arrive faster, because the model is somewhere else.

## Installing on Debian

HomeDash is one `.deb`, one binary, and it installs as three things:

- **The hub**, a system service that starts at boot and runs
  everything in this document. Closing every window changes nothing
  about what the lab is doing.
- **The panel**, a desktop entry in your applications menu that opens
  the hub's page on this machine. It opens in your browser as an
  app-style window rather than in an embedded web view, because passkeys
  have to work and the browser is where they do.
  The panel is titled with the hub's hostname — the machine's own name,
  nothing to configure — so two hubs open side by side (or through
  tunnels that both land on `localhost`) are never mistaken for one
  another.
- **The CLI**, the same `homedash` binary on any machine, signed in to
  a hub with your passkey — [working it from the command line](cli.md).
  A workstation installs the same package and never starts the service.

`apt install ./homedash_*.deb` on the box you've picked as the hub;
open HomeDash from the menu. To open it from any other machine in the
house, run `hub/packaging/https.sh` once on the hub: passkeys only work
over HTTPS off `localhost`, and it puts the panel behind
`https://homedash.local` with a certificate your devices trust once —
[opening the panel from other machines](https.md). Beyond that, nothing
else to install and nothing to configure — the first start creates the state file and the hub's key, and
fetches the one dependency the package does not carry: `omp`, at the
version this hub was tested against, into the hub's own state directory.
The hub drives `omp` over a protocol, so the version is the hub's to
choose, not the system's; it is updated with the hub and never by hand.
Both `amd64` and `arm64` builds exist.

Uninstalling removes the service, the binary and the hub's copy of `omp`,
and leaves the state directory where it was, because that is the part you
can't rebuild.

A releases page is not an apt repository, so an installed hub stays on the
version you gave it until you hand it another one — unless you
[point apt at the releases](updating.md).

## Where the state lives

State is **one database file** on the hub — hosts, clusters, workspaces,
catalog, history, tasks, peers, services, accounts, agent jobs and their
logs. No database server, no migration tooling, nothing to configure: an
absent file is created on first start. Beside it, in the same directory,
sit the two things `omp` keeps for itself: the credential vault and the
hub-side session files.

The way state survives the hub is **export**, not replication. Settings
exports the state directory as one file, encrypted under a passphrase
you type, and your browser downloads it; keep it wherever you keep
things — a USB stick, a NAS share, a friend's box. What it protects is
the part you can't rebuild by looking at the machines: which hosts are
enrolled, their pinned host keys and rebuild scripts, your clusters, your
peer approvals, your accounts and passkeys, and the vault — every
provider you signed in to. HomeDash never copies your files anywhere you
didn't ask, and a storage cluster's files are not part of an export:
pool what you can re-download.

**Restore** puts that file back. A fresh hub's setup page takes it
before any passkey exists, so a rebuilt hub comes back with the same
accounts, hosts and vault; Settings takes it on a running hub too. The
hub stages the file, checks the passphrase, then restarts itself with
the restored state. On the hub's own shell, `homedash export FILE` and
`homedash restore FILE` (with the hub stopped) do the same — see the
[entry point](../../hub/cmd/homedash/README.md#the-shells-authority).

## Is the hub well

The hub is the cheapest machine running the longest hours, so the panel
gives it what it gives every remote: a page that shows what the machine
reports about itself, never what anyone asked for. **Health**, under Hub,
is that page. The top is the box — cores, load, memory, uptime, and the
disk the state file sits on with how much of it is left. Under it is one
line per thing the hub has to keep running for the lab to work: the
agent and its credential vault, how old your last export is, the hosts heartbeating in, the schedule, the space. Each line is
green, amber or red with the reason written beside it, the page's LED in
the rail is the worst of them, and the disk line turns red at the same
fullness threshold a remote's does (`notify.disk_percent`, under
Settings › Agents). The same facts are `GET /api/hub/health` for the CLI
and for anything watching the hub from outside.

Nothing on the page is a control. What needs doing is done where it
lives — Settings for an export, Hosts for a machine, Tasks for a
failing schedule — and the page only says which.

## The API

The panel and the CLI both talk to `/api` on the hub; an unknown `/api`
path is a 404, not the panel's HTML.

## The rest of it

- [Signing in and roles](accounts.md) — passkeys, no identity provider,
  two roles, and the shell on the hub as the one authority that outranks
  the panel.
- [Working it from the command line](cli.md) — the same binary as a
  CLI on your workstation, signed in with your passkey through the
  browser, for you and for whatever assistant you already run.
- [Opening the panel from other machines](https.md) — passkeys need
  HTTPS anywhere but `localhost`; one script puts the panel behind
  `https://homedash.local` with a CA you trust once per device.
- [Keeping it updated](updating.md) — one script that puts an apt index
  in front of the releases page, so a new HomeDash arrives with
  `apt upgrade` instead of when you remember to fetch it.
- [What actually keeps the lab safe](safety.md) — four things, none of
  them a written rule: the hub keeps the keys, a job's account has no
  privilege, root is asked for through one logged door, and a job can be
  rolled back.
