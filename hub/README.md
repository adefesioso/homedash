# hub

The hub binary: everything the [docs](../README.md) describe runs in
this one process, on one small box, with one database file. This file
holds the layout and how to build; [stack.md](stack.md) holds the exact
libraries, each package's own README its internals, and the docs what
the whole thing does and why.

## Layout

Every package has a README of its own; one line here says what problem
it owns.

| Package | Problem it owns |
| --- | --- |
| [cmd/homedash](cmd/homedash/README.md) | Entry point: `serve` is the hub, no subcommand is the desktop launcher, three subcommands are the shell's authority, everything else is the CLI |
| [internal/cli](internal/cli/README.md) | The CLI: browser sign-in hand-off, the session on disk, one subcommand per panel action |
| [internal/identity](internal/identity/README.md) | The hub's SSH key, generated on first start, only the public half ever leaving |
| [internal/store](internal/store/README.md) | The one database file: open, create, schema, every table's reads and writes |
| [internal/remote](internal/remote/README.md) | The SSH executor: pinned host key, quoted arguments, scripts on stdin, reverse forwards, TCP channels |
| [internal/gate](internal/gate/README.md) | The refusal list every remote command is checked against, and the same list as an omp hook |
| [internal/fleet](internal/fleet/README.md) | Hosts: enrollment, heartbeat, metrics, the lock, the job door, what a remote is left with |
| [internal/agent](internal/agent/README.md) | omp on the hub: install and version pin, the vault, sessions in a PTY, remote jobs and their events |
| [internal/mcp](internal/mcp/README.md) | The MCP server: the fleet as typed tools for the hub's own sessions, which have no shell |
| [internal/pool](internal/pool/README.md) | Ollama on the remotes: install, the model grid, the router and its decision |
| [internal/apps](internal/apps/README.md) | Compose stacks, the catalog, and placement |
| [internal/storage](internal/storage/README.md) | Clusters and shared workspaces: exports on members, mergerfs on the gateway, shares to named remotes |
| [internal/tasks](internal/tasks/README.md) | The schedule, the runs, run now |
| [internal/peers](internal/peers/README.md) | The space: discovery, offers, cross-hub jobs, published services and their origin side, the record |
| [internal/server](internal/server/README.md) | HTTP: the API under `/api`, the guard, enrollment, service fronts, the embedded panel |
| [internal/notify](internal/notify/README.md) | The notification target: ntfy or any URL |
| [internal/backup](internal/backup/README.md) | The state directory exported as one passphrase-encrypted file, and restored from it |
| [internal/auth](internal/auth/README.md) | Passkey ceremonies, invites, tokens |
| [ui](ui/README.md) | The Svelte panel |
| [packaging](packaging/README.md) | The systemd unit, the desktop entry, maintainer scripts, `nfpm.yaml` |

## State on disk

State lives under `/var/lib/homedash` — see [state.md](state.md).

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
