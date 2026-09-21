# Windows

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

## Table

`windows` — name, created, last activity, ended.
