# Install, the vault, and state on disk

## Install and the vault

`OmpRelease` in `version.go` is GitHub's "latest release" alias, not a
pinned version. On start, if `omp/bin/omp` is missing it's fetched from
there and checked against the release's `SHA256SUMS.txt`, retried every
minute until it lands; the panel serves meanwhile. Once a binary exists,
staying current is its own job: `omp update` checks GitHub itself and
replaces itself in place. `LlmfitVersion` is still pinned in the same
file for enrollment.

Settings > Agents has an "Update oh-my-pi" button: `POST
/api/agents/update` calls `Reinstall`, which runs `omp update --force` on
the hub right away (or the bootstrap fetch, if omp isn't installed yet).
The same button walks every online remote's own `/hosts/{host}/update-omp`
(`internal/fleet`'s `UpdateOmp`), which runs `omp update --force` there
too, over SSH, touching nothing else; each finishes in the background
and is reported as an event.

Then `omp auth-broker serve --bind 127.0.0.1:8765` runs as a child of the
hub, restarted if it exits: the only holder of refresh tokens and the
only refresher. Remotes reach it through the reverse forward on a job's
connection with the token in `omp/auth-broker.token`, delivered over SSH.

Every omp process on the hub runs with `HOME` = the state dir and
`PI_CONFIG_DIR=omp`, so everything omp writes lands under `omp/`.

## State on disk

```
omp/
  bin/omp                 the pinned binary
  auth-broker.token
  cache/                  omp's encrypted credential snapshot
  windows/                the empty directory every window starts in
  agent/
    agent.db              the vault: every provider credential, refresh tokens included
    config.yml            omp's own settings, written by its setup
    mcp.json              the hub's MCP server with the hub-agent token: a window's only tool source
    models.yml            the hub's router as provider "homedash", with the same token
    AGENTS.md             the hub agent's standing instructions, rewritten on every start
    sessions/             hub-side window sessions, JSONL
```
