# Inference

## Pooling the GPUs

*A machine with a GPU sits idle most of the day while you pay a per-token
API to answer questions.*

The enrolled remotes become a pool of Ollama machines, and the hub hands
out **one endpoint** for all of them. This is the spine of the whole
project: everything in [part two](../sharing/README.md) is this router,
offered to someone else.

- **Install Ollama** on a card installs it and makes it reachable by the
  hub; unchecking removes all of it. A card can also take its machine out
  of the pool without removing anything.
- The router speaks Ollama's own shape, so existing clients work
  unchanged, under the same credentials as the rest of the panel.
- It picks a machine that's enabled, reachable, idle and has the model,
  one request per machine at a time. When all are busy, requests queue to
  a depth you set. Streaming requests stream.
- A request names a model and matches it exactly, never a substitute.
- Turns after the first in a conversation go back where the first one
  went — see [one decision per conversation](#one-decision-per-conversation).

This is also the seam part two plugs into: when the local pool can't
place a request, a peer's pool gets first refusal before the queue.

## Getting the models onto the machines

*Placement wants a machine that has the model, and the only way to put a
model on a machine is to SSH into it — the exact thing the panel exists to
stop you doing.*

The Models tab is a grid: every Ollama machine down one side, every model
any of them holds across the other. Pull a model to one machine or to
several at once and the cells fill in as it lands; remove one to get the
disk back. **A pull that fails says why**: Ollama answers a bad model
name inside the stream rather than with a failed request, so the row
shows that line's reason instead of just disappearing, and stays up
long enough to read it. A pull started here keeps going, and keeps
showing, even if you switch to another tab and back before it lands.

It answers two questions the router can't phrase on its own: which models
the pool can serve at all — the same list this hub advertises to its space
— and why a request queued or a peer's job came back refused, when the
answer is that the one machine holding that model was busy, or that
nothing in the pool holds it. Both read as a gap in the grid rather than
as an error you have to interpret. Each machine's column totals against
its free space.

**A connected peer gets a column too**, read-only: what it currently
offers, not what this hub owns. There's nothing to pull or remove — only
the peer's own panel can do that — so the column just says what's
reachable through the space right now. It's also folded into the model
picker: [oh-my-pi's own discovery](agents/README.md#models-and-providers)
against the `homedash` provider lists a peer's models alongside this
hub's, since placement above already reaches them through a peer when
nothing local holds them — a picker naming one names a real choice. None
of this changes what this hub offers *to* its space: an outbound offer
and an inbound job both still answer only from machines this hub owns,
never a peer's, so nothing crosses two hops.

**What fits.** Every column has a *What fits* action: the hub asks
[llmfit](https://github.com/AlexsJones/llmfit) on that machine —
installed by enrollment beside the agent — to rank models against the
memory, CPU and GPU it actually has, and lists the top fits with a Pull
button on each and the speed it expects. A machine's card uses the same
answer to suggest a model when you point its agent at the pool, so "should
this box think locally" is answered by the box, not guessed.

## One decision per conversation

*A chat is not one job. It is turn after turn carrying the same history
forward, and a router that decides again on every turn sends each one
somewhere new, where the conversation is a stranger and every token before
it is read from scratch.*

The machine that just answered a turn is holding that conversation warm.
So the choice is made once, on a conversation's first turn, and every turn
after it goes to the same place — the same peer, or the same local machine
— for as long as that place is still there and still says yes. When it
isn't, the next turn is placed as a first turn and becomes the new home.

No client is asked to carry a session id, because none of them do.
Conversations identify themselves by their history: the hub recognizes a
turn whose messages begin with a history it has already placed, which is
exactly the thing the warm cache is keyed on anyway.

**The decision is visible.** Every response the router returns names
where it was placed and the one reason it went there — the local pool was
busy, this peer had the fastest record, this was the conversation's home
from turn one. It travels as the `X-HomeDash-Decision` header, a small
JSON object: `host` or `peer` names where it landed, `reason` says why. A
peer that was asked and said no still gets named: `peer` carries its name
and `peerError` carries its own words, even when the answer ends up a
local 404 or the queue — an attempted peer is never silently dropped from
the decision. A response only ever claims 200 once the peer's own answer
has actually started arriving, so a relay reset between the peer's yes
and its first byte is a failure, never an empty success.
