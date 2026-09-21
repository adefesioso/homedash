# internal/server

The hub's HTTP surface: the API under `/api`, the router at Ollama's
paths, the MCP server at `/api/mcp`, the service fronts, and the embedded
panel everywhere else. Panel, windows and the CLI are three front doors
onto these same handlers.

## The guard

`guard` wraps everything. `/api/health`, `/api/auth/*` and the panel's
static files are open, and so is `POST /api/backup/restore` while the hub
has no account yet (the rebuild case: an export carries its accounts); every other `/api/*`, `/v1/*` and `/~*` path needs
an account — a session cookie, or a bearer that is either an API token
(`hd_…`) or a session token, which is how the CLI signs — and a viewer may only
read, except the router (a prompt changes nothing), signing out, and a
link front (its app takes POSTs). A viewer's PTY has no keyboard.

## Routes

Handlers live in the file named for the feature: `auth.go` (sign-in,
users, invites, tokens, and the CLI grant: `POST /api/auth/cli/grant`
needs a signed-in session and returns a one-time code, `POST
/api/auth/cli/redeem` is open and trades the code for a session), `hosts.go` (hosts, enrollment, metrics, lock,
run, a file write, credentials and their revocation, re-provision, rebuild
script, agent model, fit, and the enrollment listener), `secrets.go`, `pool.go` (Ollama install, pool toggle, the
grid, pull, delete), `apps.go` (stacks, catalog, placement),
`storage.go` (clusters, members, workspaces), `backup.go` (the export
download, the restore upload that stages and restarts), `peers.go`
(the tab, controls, forget, services, fronts), `tasks.go`, `jobs.go`,
`agents.go` (the agent's status, its providers and models, windows, the PTY WebSocket) and
`health.go` (`/api/hub/health`: the hub's own machine, process and state
file read the way a remote's facts are, and one line per thing it has
to keep running, each ok/warn/bad/off with the reason — the Health tab
and the rail's LED both read it), and
`server.go` (`/api/health` for the launcher, `/api/hub`, events, settings).

The panel's files are served from the binary with `Cache-Control`: the
hashed `assets/*` are immutable for a year, `index.html` is `no-cache`,
so a new hub build is picked up on the next load and nothing else is
downloaded twice.

`settingKeys` in `server.go` is the whole of what the panel may read or
write through `/api/settings`; a key outside it is refused rather than
stored. Every `{host}` in a path resolves through the store's host
lookup, which is the structural half of the gate.

## Service fronts

`fronts.go` is a peer's service, fronted here.

- **Link.** `/~{peer}/{service}/…` is a `httputil.ReverseProxy` whose
  transport dials `Peers.DialService` instead of a socket; the prefix is
  stripped and sent as `X-Forwarded-Prefix`. It needs the front approved
  in this hub's `fronts` table and a signed-in user of either role.
- **Ingress.** While any approved front carries a hostname, `ingress`
  keeps a listener on `:443` with `acme/autocert` (cache in `acme/` under
  the state dir, host policy = those hostnames) and one on `:80` for the
  HTTP-01 challenge, routing by `Host` to the same kind of proxy with no
  sign-in of its own. It starts and stops as hostnames appear and go; a
  bind that fails is one `ingress.failed` event and a retry each minute.
  Binding those ports needs `CAP_NET_BIND_SERVICE`, which the
  [unit](../../packaging/README.md) grants.

Every connection through a front is counted in `peer_bytes` as
`fronted`. A front never serves a service this hub is itself reaching
through a peer: a front is by construction a peer's own service, and a
service row names only a host this hub owns.
