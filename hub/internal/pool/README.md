# internal/pool

The GPUs: every enrolled remote with Ollama on it, behind one endpoint.

**Install** puts Ollama on a host with its own installer and a systemd
drop-in that makes it listen on `0.0.0.0:11434` for the hub; **Remove**
takes all of it away. Both append to the host's rebuild script. A host
can also be taken out of the pool without removing anything
(`pool.disabled.<id>` in settings).

**The grid** asks every Ollama machine `/api/tags`, live, cached ten
seconds so the router does not ask on every attempt; **Pull** streams a
pull straight through to the browser, **Delete** removes a model, and
both note it in the rebuild script. The server layer adds a read-only
column per connected peer (from `Peers.Rows`) before handing the grid to
the panel — [peers](../peers/README.md) owns those machines, so there's
nothing here to pull to or delete from one.

**The router** (`Serve`) is mounted on the hub's listener at Ollama's own
paths — `/api/tags`, `/api/chat`, `/api/generate`, `/api/embed`,
`/api/embeddings`, `/api/show`, `/api/version` and `/v1/*` — so a client
configured for Ollama points at `http://hub:7433` unchanged. It places in
this order, and every response carries `X-HomeDash-Decision`, where it
went and the one reason:

1. The conversation's home, if it is still there and still says yes.
   Homes are hashes of message prefixes kept in memory for two hours; a
   turn whose messages begin with a history already placed goes where
   that history went.
2. A free local machine that is enabled, online and holds the model —
   one request per machine at a time.
3. A peer, through `Peers.Place`, before the queue — and at once when
   nothing local holds the model, which is what makes a hub with an
   empty grid a gateway. A request marked `X-HomeDash-Local-Only` (one
   that arrived from a peer) never reaches this step.
4. The queue, to `router.queue` (8) deep, for `router.queue_wait` (120)
   seconds. A queued request sleeps until a machine is released, and
   tries again then; it does not poll.

A model is matched exactly (`name` or `name:latest`), never substituted.
Streaming responses are flushed per chunk.

`/api/tags` and `/v1/models` — the router's own discovery, what omp's
`homedash` provider and the Settings picker read — list this pool's
models unioned with `Peers.Offered`, so a peer-only model is a real,
pickable choice: placement (step 3 above) already reaches it. This union
is only for that listing; the outbound offer and `handleJob`'s check of
what to accept both call `Models` directly and stay local-only, so
nothing this hub doesn't own gets relayed a second hop.
