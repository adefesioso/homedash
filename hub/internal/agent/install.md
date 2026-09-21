# Install, the vault, and state on disk

## Install and the vault

`OmpVersion` in `version.go` pins the release this hub was tested
against. On start the pinned binary is fetched into `omp/bin/` if absent
or the wrong version, checked against the release's `SHA256SUMS.txt`,
and retried every minute until it lands; the panel serves meanwhile.
`LlmfitVersion` is pinned in the same file for enrollment.

Settings > Agents has an "Update oh-my-pi" button: `POST
/api/agents/update` calls `Reinstall`, which redoes that fetch and check
on the hub right away rather than waiting on the minute backoff — for a
corrupted binary or a stuck retry. A remote only ever gets a newer
pinned version through its own Re-provision (`internal/fleet`'s
`layout.sh`), so the same button walks every online host's `/reprovision`
from the panel; each finishes in the background and is reported as an
event, same as clicking Re-provision by hand.

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
