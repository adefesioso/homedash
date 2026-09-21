# internal/cli

The `homedash` binary on a workstation: one subcommand per thing the
panel can do, over the hub's own API, signed in as a person. What it is
for and the command list are in
[the docs](../../../docs/running/cli.md); this file is how it works.

## Signing in

`homedash login <hub-url>` listens on a random loopback port, opens the
browser at `<hub>/#cli?port=P&state=S` and waits. The panel, once the
person has signed in with their passkey, shows who is asking and an
Approve button; approving calls `POST /api/auth/cli/grant` with the
panel's own session and sends the browser to
`http://127.0.0.1:P/?code=C&state=S`. The CLI checks the state it
minted, trades the code at `POST /api/auth/cli/redeem` for a session
token, and writes `~/.config/homedash/hub.json` (mode 0600) with the hub
URL, the token, the account's name and role. `homedash logout` deletes
the session on the hub and the file. `homedash whoami` prints the file
minus the token.

The session is the same thirty-day row the panel gets. There is no
refresh; when it expires the CLI says so and asks for `login` again.

`HOMEDASH_HUB` and `HOMEDASH_TOKEN` override the file, for a script that
holds an [API token](../auth/README.md) instead.

## Layout

- `config.go` — the file, its path (`XDG_CONFIG_HOME` honoured), load
  and save.
- `client.go` — one `http.Client` with the bearer, JSON in and out, and
  the hub's error text surfaced as the CLI's.
- `login.go` — the loopback listener and the browser hand-off.
- `commands.go` — the subcommand table: name, usage, and a function
  that turns arguments into one API call and prints the answer; `--json`
  prints the body untouched, otherwise a short table or the text itself.

Every write goes through the same handlers the panel uses, so the
[gate](../gate/README.md) and the [guard](../server/README.md#the-guard)
apply unchanged; the CLI adds no path of its own to the hub except the
two under `/api/auth/cli/` and `PUT /api/hosts/{host}/file`.
