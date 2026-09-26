# Models and providers

A **fleet default** — provider and model — is set in Settings and applied
to every remote at enrollment. Any card can override it — **Change**
beside **Agent** on the host card opens the same provider-and-model
picker Settings has, with *fleet default* as the first choice — because
a machine with a GPU should think locally and a Pi-class box should not,
and the house's own [pool](../inference.md) is a provider like any other:
point a remote at the hub's router and its agent runs on the fleet's
GPUs. The card shows the model the remote last
reported it was configured for, never what the hub asked for. A model is
always `provider/model` — the model half is the provider's own id and
may itself contain slashes (`openrouter/openai/gpt-4o-mini`).

The `homedash` provider's picker isn't only this hub's own machines: a
model a connected peer currently offers appears too, since the router
already [places a request there](../inference.md#getting-the-models-onto-the-machines)
when nothing local holds it — for the hub's agent. **A job never goes to
a peer**: a root agent must not follow a stranger's model, so a job's
router is local-only, and a model only a peer holds is refused.
