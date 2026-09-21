# Inference across the space

## How a job travels

When the local router can't place a request and the queue would take it,
it asks the peer list first. A peer is a candidate when it is approved
here and its latest offer fits the job — it lists the model and reports
a free machine. Which
candidate gets it is decided by [the record](#choosing-a-peer), not by the
order of the list.

1. **A sends B a header** — the model and the path, a few bytes — over
   the encrypted connection it already holds. Nothing between them sees
   anything but ciphertext.
2. **B verifies the sender** by the connection's public key, which the
   handshake binds to whoever holds the matching private key.
3. **B decides.** Unapproved, over its concurrency limit, over its
   hourly allowance or the hub's, or a path that is not chat, generate
   or embed, and B refuses with a reason — A stops waiting and falls
   back to its own queue. Only after a yes does A send the prompt, so a
   hub B has not approved never gets to make B read anything.

**Only prompts leave the house.** A model lookup — `/api/show`,
`/api/tags`, `/api/version` — is answered at home, from whatever the
local pool already knows, and never asked of a peer: those are not
listed above because B would refuse them anyway, and asking first would
just be a slower way to find that out. A `/api/show` for a model nothing
local holds gets Ollama's own 404 rather than a queue or a peer's
refusal.
4. **B routes the job locally**, through exactly the placement path a
   request of its own takes.
5. **B streams the result back** as it arrives, and **A reassembles** it
   into the response its own client has been reading all along. A never
   learns which of B's machines ran it.

**A job that arrived from a peer is never forwarded onward.** B serves it
on its own machines or refuses it. One hop, always.

## Choosing a peer

*Every candidate is claiming the same two things — I have that, I have a
free machine — in the same words. A claim from a hub that refused the last
four jobs sent to it is not worth the same as that sentence from one that
has answered every time, and nothing in the offer says which is which.*

An offer is what a hub says about itself. What it has actually done is
written down next to it, so the choice uses both: among the candidates,
the one with the better recent record goes first. The record is three
numbers per peer, each decayed so that last week stops counting — the
share of jobs it accepted rather than refused, the share that finished
rather than died mid-stream, and the median time to its first token.

The last of those is what makes the choice worth making at all. Every peer
in a space is somebody's spare hardware, and the spread between a 4090 two
streets away and a laptop on a hotel connection is not subtle. Both hubs
can tell which they're talking to, after one job.

A peer with no record yet is tried rather than skipped, and a peer that
has been failing is demoted rather than dropped, so a hub that was down
for an afternoon earns its way back without anyone touching the Peers tab.

**The decision is visible.** Every response the router returns names where
it was placed and the one reason it went there. A router nobody can see
into is one nobody can tune, and the whole of part two is a router in
somebody else's house.

## One door to many labs

*Every hub in a space is a full participant, which is more than most of
them need to be. A household with no GPU at all still has questions; a
group that pooled five labs still wants one address to send work to,
not five.*

Nothing has to be added for a hub to be that address, because it already
is one: a hub's router tries its own machines, then its space, and a hub
with **no machines of its own** simply goes straight to its space. A
HomeDash with an empty grid and a space name is a **gateway** — one
endpoint, one set of credentials, one decision line, and behind it every
model every approved lab in the space is offering.

It stays inside every rule above:

- **One hop.** The gateway is the client's hub, so its job to a peer is
  the first hop, not a forward.
- **The peer still decides.** Approval, quotas and the record all sit on
  the receiving side. A gateway can ask any lab; it can compel none.
- **The client decides what, the record decides where.** The client never
  learns which lab ran it.
- **Nothing is advertised that isn't owned.** A gateway with no machines
  makes no offer, so it cannot re-export the space to itself or to anyone.
