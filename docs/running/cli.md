# Working it from the command line

*You know what you want — "find whatever is filling the NAS", "put
Immich somewhere with room for the photos" — and you are already sitting
at a terminal, or an assistant is, and typing it is faster than clicking
it.*

The same binary that runs the hub is the **CLI** on your workstation.
`homedash login https://hub.lan` signs in with the
[passkey you already have](accounts.md): the terminal cannot do
WebAuthn, so the CLI opens your browser at the hub, you pass your
passkey there and approve *this* computer, and the browser hands the
result back to a port the CLI is listening on. Nothing is pasted, nothing
is shown once, and the session it leaves in `~/.config/homedash/` is the
same thirty-day session the panel has — signing out of it, from either
end, ends it. A viewer's CLI reads; an admin's does everything below.

From then on the fleet is a set of subcommands:

```
homedash hosts                              every enrolled machine and its status
homedash devices                            every device the remotes can see
homedash storage                            clusters, members, health; workspaces
homedash apps [HOST]                        stacks live from every remote
homedash logs HOST STACK [-n LINES]         a stack's recent logs
homedash catalog                            the app catalog
homedash place '{"memoryMB":8192}'          rank the fleet for a stack's needs
homedash fit HOST [-n N]                    what llmfit says fits on a machine
homedash secrets                            the secret names a host may use, never a value
homedash events [--before ID] [--limit N]   the event log, newest first (200; --before pages older)
homedash jobs [HOST]                        recent jobs
homedash job ID [--after EVENT-ID]          one job: state, report, newest events
homedash run HOST [-t SECONDS] -- CMD…      one gated command on one host
homedash write HOST PATH [-f FILE] [--sudo] a file on a host, from -f or stdin
homedash lock HOST on|off                   the SSH lock
homedash name DEVICE NAME [KIND]            your name on a device, beside the guess
homedash deploy HOST NAME -f COMPOSE        install or update a stack
homedash app HOST NAME start|stop|…         start, stop, restart, pull, remove
homedash start HOST [--cwd DIR] TEXT…       give a remote's agent a job
homedash correct ID TEXT…                   a follow-up round on a job
homedash rollback ID                        put a host back to the snapshot taken before a job
homedash reprovision HOST                   run the enrollment layout again on a host
homedash rebuild HOST [-f SCRIPT]           read, or replace, a host's rebuild script
homedash whoami · homedash logout           which hub, as whom · end the session
```

Every subcommand takes `--json` and prints exactly what the hub's API
returned, so an assistant — Claude Code, or anything with a shell —
drives the lab with the same commands you do and reads the same answers.
There is no separate integration to install and no token to hand it: the
CLI is signed in as *you*, on this machine, and an assistant here is you
at the keyboard. Point it at [safety](safety.md) to know what will be
refused and why.

The hub's own sessions on the [Agents tab](../pooling/agents/sessions.md)
are the one client that does not use the CLI: they have no shell at all,
and reach the same fleet as [typed tools](../../hub/internal/mcp/README.md)
over a small MCP server with a token of their own. They are not
privileged by that; every tool names a host and resolves it through the
database, so [the safety gate](safety.md) applies to the session in the
panel precisely as it applies to your terminal.

The panel remains complete on its own — everything in these documents is
clickable — so the CLI is an accelerator, never a dependency.
