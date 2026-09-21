# internal/apps

Compose stacks on the remotes, and where a new one should go.

**Stacks are not a table.** A stack is `~/stacks/<name>/compose.yml`
(plus an optional `.env`) under the hub's account on the remote, and
`List` is `docker compose ls` plus `docker ps` on every online host,
live. A stack whose file is elsewhere was deployed by hand; it is listed
and can be started, stopped and removed, but not read or updated from
the hub. `Deploy` writes the files and brings the stack up (update is
the same call); `Action` is start, stop, restart, pull or remove, with or
without volumes; `File` and `Logs` read a managed stack's files and its
containers' recent output. A deploy is appended to the host's rebuild
script.

**The catalog** ships empty; it is only the entries you add, stored as
JSON in the `catalog` table by name — each a compose file, the volumes it
wants and a rough requirements line. Nothing about a catalog entry is
privileged. The hub agent adds to it through the
[MCP server](../mcp/README.md), from a stack it has running.

**Placement** (`Place`) is arithmetic: for each online host it scores the
stack's needs — cores, memory, disk, GPU — against the host's cores,
available memory, free disk and the root mount's free-space trend over
the last week from the metrics the heartbeat kept, requires margin
rather than an exact fit, and returns one verdict per host with the
sentence that says why. A stack that names a cluster belongs on that
cluster's gateway, which placement asks the storage package for.
Publishing a stack's port is the [server](../server/README.md)'s and
[peers](../peers/README.md)' business; this package knows nothing about
it.
