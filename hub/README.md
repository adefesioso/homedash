# hub

The hub binary: everything the [docs](../README.md) describe runs in
this one process, on one small box, with one database file. This file
holds the stack, the layout and how to build; each package's README holds
its own internals, and the docs hold what it does and why.

## Stack

| Concern | Choice |
| --- | --- |
| Language | Go, one static cgo-free binary; `go:embed` carries the panel |
| State | SQLite via `modernc.org/sqlite` — [store](internal/store/README.md) |
| SSH | `golang.org/x/crypto/ssh`, host keys pinned — [remote](internal/remote/README.md) |
| Peer network | `go-libp2p` on a Kademlia DHT, the public one or a private one of hubs — [peers](internal/peers/README.md) |
| Inference router | `net/http` streaming proxy in Ollama's shape — [pool](internal/pool/README.md) |
| Scheduler | `robfig/cron/v3` in-process — [tasks](internal/tasks/README.md) |
| Sign-in | `go-webauthn/webauthn` passkeys, two roles — [auth](internal/auth/README.md) |
| Session tools | `modelcontextprotocol/go-sdk` at `/api/mcp` — [mcp](internal/mcp/README.md) |
| CLI | `net/http` client on the same API; `flag` for parsing — [cli](internal/cli/README.md) |
| Agent | [oh-my-pi](https://github.com/can1357/oh-my-pi) `omp`, pinned — [agent](internal/agent/README.md) |
| Model fit | [llmfit](https://github.com/AlexsJones/llmfit), pinned — [fleet](internal/fleet/README.md) |
| Storage clusters | mergerfs over NFS on the remotes — [storage](internal/storage/README.md) |
| Service fronts | `net/http` reverse proxy; `acme/autocert` for an ingress — [server](internal/server/README.md) |
| Export encryption | `filippo.io/age` (passphrase) — [backup](internal/backup/README.md) |
| Panel | Svelte 5 + Vite, static, embedded — [ui](ui/README.md) |
| Packaging | nfpm → `.deb` for `amd64` and `arm64` — [packaging](packaging/README.md) |

## Layout

Every package has a README of its own; one or two lines here say what
problem it owns.

- [cmd/homedash](cmd/homedash/README.md) — the entry point: `homedash
  serve` is the hub, `homedash` alone is the desktop launcher, three
  subcommands are the shell's authority over sign-in, and everything
  else is the CLI.
- [internal/cli](internal/cli/README.md) — the CLI: the browser
  hand-off that signs it in, the session on disk, and one subcommand
  per thing the panel can do.
- [internal/identity](internal/identity/README.md) — the hub's SSH key,
  generated on first start, of which only the public half ever leaves.
- [internal/store](internal/store/README.md) — the one database file:
  open, create, schema, and every table's reads and writes.
- [internal/remote](internal/remote/README.md) — the SSH executor: the
  one door to a remote, with pinned host key, quoted arguments, scripts on
  stdin, reverse forwards and TCP channels.
- [internal/gate](internal/gate/README.md) — the refusal list every
  remote command is checked against, and the same list as an omp hook.
- [internal/fleet](internal/fleet/README.md) — hosts: enrollment, the
  heartbeat and its fact script, metrics, the lock, the job door, and
  what a remote is left with.
- [internal/agent](internal/agent/README.md) — omp on the hub: install
  and version pin, the vault as a child process, sessions in a PTY, remote
  jobs and their event stream.
- [internal/mcp](internal/mcp/README.md) — the MCP server: the fleet
  as typed tools for the hub's own sessions, which have no shell.
- [internal/pool](internal/pool/README.md) — Ollama on the remotes:
  install, the model grid, the router and its decision.
- [internal/apps](internal/apps/README.md) — compose stacks, the
  catalog, and placement.
- [internal/storage](internal/storage/README.md) — clusters and shared
  workspaces: exports on the members, mergerfs on the gateway, shares to
  the named remotes.
- [internal/tasks](internal/tasks/README.md) — the schedule, the runs,
  run now.
- [internal/peers](internal/peers/README.md) — the space: discovery,
  offers, jobs across hubs, published services and their origin side,
  and the record.
- [internal/server](internal/server/README.md) — HTTP: the API under
  `/api`, the guard, the enrollment listener, the service fronts and the
  embedded panel.
- [internal/notify](internal/notify/README.md) — the notification
  target: ntfy or any URL.
- [internal/backup](internal/backup/README.md) — the state directory
  exported as one passphrase-encrypted file, and restored from it.
- [internal/auth](internal/auth/README.md) — passkey ceremonies,
  invites, tokens.
- [ui](ui/README.md) — the Svelte panel.
- [packaging](packaging/README.md) — the systemd unit, the desktop entry,
  the maintainer scripts, `nfpm.yaml`.

## State on disk

`/var/lib/homedash` (`HOMEDASH_STATE`) holds `homedash.db`, the hub's
key pair, `peer_key`, `secrets.key`, `acme/` for ingress certificates and
`omp/` — the hub's own omp root, laid out in the
[agent README](internal/agent/README.md#state-on-disk). An export carries
the whole directory, not just the database: the vault is as unrebuildable
as the pinned host keys. A restore staged under `restore.pending/` is
applied at the next start, before the store opens.

## Building

Requires Go ≥ 1.24, Node ≥ 20 and `nfpm`. Then:

```
make            # builds the panel, then the binary into dist/
make deb        # dist/homedash_<version>_amd64.deb and _arm64.deb
make run        # serve from a scratch state dir on a local port
```

The version is the git tag, or `0.0.0+<commits>.g<hash>[.dirty]` for an
untagged tree, so a dev build is always newer than the last. To exercise
the packages against a whole fleet, [`../lab`](../lab/README.md) stands
up a nested Proxmox with two NATed houses and installs the `.deb` on both
hubs.
