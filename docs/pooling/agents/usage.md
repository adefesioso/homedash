# Usage

*A remote the dispatcher skips looks exactly like one that's working, until you count what each spent.*

The Usage tab counts every token on both sides, over 24h, 7d or 30d:

| Side | Who | Counted from |
| --- | --- | --- |
| Spent | The hub agent | Its windows' omp session files, read once a minute past the last offset |
| Spent | Each remote's agent | A job's `message_end` usage ([jobs](jobs.md)) |
| Served | Each local Ollama machine, each peer | The router's answer stream: Ollama's final counts or OpenAI's `usage` |

- A served request names who asked: the hub agent, a remote's agent (the job door), a peer, or another signed-in client.
- A reply from the house's own pool shows twice by design: spent by the asker, served by the machine.
- Served tokens split into answered by peers and answered for peers — the space's give and take.
- Cost is the provider's own estimate; a pool model costs nothing.

Internals: [agent](../../../hub/internal/agent/README.md), [pool](../../../hub/internal/pool/README.md).
