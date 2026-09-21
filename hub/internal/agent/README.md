# internal/agent

omp on the hub, and omp on the remotes driven the same way. The hub
never runs a tool of its own; a window's only tools are the hub's MCP
server, and a job is omp on a remote with that remote's own tools.

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

## Providers and models

`Models` runs `omp models --json` with the hub's own environment, so the
answer is what the vault can actually run: every provider the vault holds
a credential for (plus `homedash`, the house's pool), each with its model
ids as omp names them. `GET /api/agents/models` serves it, cached for a
minute. Settings builds its hub and remote model pickers from it — a
provider chosen from that list, a model id typed or picked beside it —
because a model id alone does not say which provider runs it
(`openai/gpt-4o-mini` is OpenRouter's tag for an OpenAI model, and the
setting must read `openrouter/openai/gpt-4o-mini`).

## Windows

`Open` names the window — `adjective-noun` from two short word lists
(`names.go`), redrawn until it is unique among the live windows — records
a `windows` row and starts `omp --no-tools --model <hub model>` in a PTY
(`agent.default_model` in Settings). The PTY outlives any WebSocket
attached to it: closing the browser detaches, reopening reattaches and
replays the last 64 KB. Keyboard input counts as activity, recorded at
most every few seconds. When the process ends, that same 64 KB is written
to the row (`windows.scrollback`) and `GET /api/agents/sessions/{id}/history`
serves it raw for the panel's read-only terminal; a window whose process
has ended stays in the list as history. After a hub restart every window
is history (`EndAllWindows`) with no scrollback — the process took its
buffer with it — its session file still on disk.

`AGENTS.md` says what the agent is, that its tools are the fleet, that
work goes to a remote as a job by default and `run_command`/`write_file`
are for looking (or for the three cases a job cannot cover: a host
without an agent, the house's network, one root command past
`homedash-sudo`), what a job may write and reach so its instructions
are written around those limits, how a
job is given and read, that a workspace path can be a job's working
directory, that the catalog is where a deploy starts and where a stack
that worked is saved, and that after each report it updates that host's
rebuild script — small data inline as heredocs, larger data named at the top as
what the script can't recreate.

## Remote jobs

`Jobs.Start` refuses a host under the memory floor, without an agent, or
without the agent account (re-provision), records a `jobs` row, and runs
one round in the background. A round is one SSH connection with the
fleet's forwards up; on it, in order: `snapshot.sh snapshot <id>` as root
(btrfs subvolume or LVM snapshot of `/`, or `none`; the kind lands on the
row), then `omp --mode json -p --cwd <dir> --model <provider/id>
--approval-mode yolo --max-time <t>` (the model: the host's own
override, else `agent.remote_model`, else the hub's
`agent.default_model`) — not as the account the hub logged
in as, but inside `sudo systemd-run` as `homedash-agent`, with
`NoNewPrivileges`, `ProtectSystem=strict`, `PrivateTmp`,
`RestrictSUIDSGID`, `TasksMax`, `RuntimeMaxSec`, the optional
`jobs.memory_max` and `jobs.cpu_quota`, and `ReadWritePaths` for the
account's home (and so whatever is mounted under it), the working
directory, the cluster this host is the gateway of and every workspace
it is a member of (`sharedPaths`, each with systemd's `-` prefix so an
unmounted one is skipped, not fatal); the prompt on stdin. Every
JSON line omp emits is a `job_events` row, written in batches (every
quarter second or fifty lines, one transaction) so a chatty job is not
one fsync per line; the last assistant message is
the report; a `sudo` line is appended for every `homedash-sudo` the door
served. The hub prepends one paragraph to every job's text: it has no
root, it has docker, what it can write, `homedash-sudo` is how to ask for root (a package, a
service, a mount, a data disk to format and add to fstab — never the system disk), end with a report listing
every change as the commands that would make it again, name data that
should survive a rebuild — plus the names of the secrets this host may
read. `Correct` is `--resume <session>` on the remote's own session,
capped by `agent.rounds` (3); at the cap the job is `needs_you`.
`Rollback` runs `snapshot.sh rollback <id>` on a finished job. Retention
`jobs.retention` (20) is applied per host after every round, and a
trimmed job's snapshot is deleted with it.

## Tables

`windows` — name, created, last activity, ended. `jobs` — host, working
directory, the hub-side window that started it, model, text, round
count, state (`running`, `done`, `failed`, `needs_you`), timeout, the
remote's session id, the snapshot kind (`btrfs`, `lvm`, `none`, or
`restored`), report, reason, started/ended. `job_events` — one
row per line, in order.
