# internal/mcp

The hub's MCP server: the fleet as typed tools over streamable HTTP at
`/api/mcp`, stateless, behind the same guard as the panel. One client
gets them: the hub's own omp sessions, which run with no shell and reach
the fleet only through these tools, with a token of their own. A
workstation uses the [CLI](../cli/README.md) instead. The session is not
privileged by its tools: every one that touches a machine names a host
and resolves it through the store, so the gate applies to it exactly as
to the CLI.

| Tool | Does | Marked |
| --- | --- | --- |
| `list_events` | the event log, newest first | read-only |
| `list_hosts` | every enrolled machine with its status and facts | read-only |
| `list_devices` | every device the remotes can see, with the guess, your name, and who last saw it | read-only |
| `list_storage` | every cluster with its members and health, and every workspace with its path and the remotes it appears on | read-only |
| `list_apps`, `app_logs`, `catalog` | stacks live from every remote, a stack's recent logs, the catalog — the agent's first stop before a deploy | read-only |
| `placement` | rank the fleet for a stack's needs | read-only |
| `model_fit` | what llmfit says fits on a machine | read-only |
| `list_secrets` | the secret names a host may ask for, never a value | read-only |
| `rebuild_script_get` | a host's rebuild script | read-only |
| `list_jobs`, `job_status` | jobs, and one job's state, report and newest events | read-only |
| `run_command` | one gated command on one host | destructive |
| `write_file` | a file on a host (SSH config paths refused) | destructive |
| `lock_host` | the SSH lock on or off | destructive |
| `name_device` | your name and kind on a device, beside the guess | destructive |
| `deploy_stack`, `app_action` | install or update a stack; start, stop, restart, pull, remove | destructive |
| `catalog_add` | save a catalog entry from compose text, or from a stack installed on a host (its compose read back from the remote, never its `.env`) | destructive |
| `start_job`, `job_correct` | give a remote's agent a job; a follow-up round | destructive |
| `job_rollback` | restore a host to the snapshot taken before a job | destructive |
| `rebuild_script_set` | replace a host's rebuild script | destructive |

Tools land in the file named for the feature that owns them (`hosts.go`,
`devices.go`, `jobs.go`, `apps.go`, `storage.go`) and register through
`addTool`, never the SDK directly: the SDK validates every response
against a schema it infers from the Go type, and would read the raw JSON
the hub stores as-is (a host's facts, a sighting's detail, llmfit's system
block) as an array of bytes and reject its own output. `addTool` writes
those fields as "any JSON".
