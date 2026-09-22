# internal/agent

omp on the hub, and omp on the remotes driven the same way. The hub
never runs a tool of its own; a window's only tools are the hub's MCP
server, and a job is omp on a remote with that remote's own tools.

## Install and the vault

The pinned omp/llmfit binaries and the auth-broker that alone holds
refresh tokens. [install.md](install.md)

## State on disk

The `omp/` layout everything the hub's own omp writes lands under.
[install.md](install.md)

## Providers and models

`Models` runs `omp models --json` with the hub's own environment, so the
answer is what the vault can actually run: every provider the vault holds
a credential for (plus `homedash`, the house's pool), each with its model
ids as omp names them. `GET /api/agents/models` serves it, cached for a
minute. Settings builds its hub and remote model pickers from it — a
provider chosen from that list, a model id typed or picked beside it —
because a model id alone does not say which provider runs it
(`openai/gpt-4o-mini` is OpenRouter's tag for an OpenAI model, and the
setting must read `openrouter/openai/gpt-4o-mini`). A window opening
reads this same cache to warm omp's own on-disk provider cache before
the TUI starts, rather than deleting and rebuilding it every time — a
stale entry (a new credential, a pool host that just came online) is
caught up to on the next minute's expiry, or right away with `POST
/api/agents/models/refresh` ("Refresh model cache" under Settings >
Agents), which deletes omp's on-disk cache and forces the discovery.

## Windows

A hub-side PTY running omp with no tools, named, reattachable, its
scrollback kept after the process ends. [windows.md](windows.md)

## Remote jobs

A job is one SSH round: snapshot, `omp` run as `homedash-agent` under
`systemd-run` with a locked-down unit and only the paths it needs,
events streamed back, retention, rollback, and a kill switch.
[jobs.md](jobs.md)
