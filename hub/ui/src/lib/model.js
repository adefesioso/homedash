// A model setting is "provider/model" on the wire — the model half is the
// provider's own id and may itself hold slashes
// (openrouter/openai/gpt-4o-mini); homedash/<model> for the house's own
// pool matches the same shape. ModelPick edits it as {provider, model}.
export const splitModel = (v) => { const i = (v || '').indexOf('/'); return i < 0 ? { provider: '', model: v || '' } : { provider: v.slice(0, i), model: v.slice(i + 1) }; };
export const joinModel = (p) => (p.provider && p.model.trim() ? `${p.provider}/${p.model.trim()}` : '');
// halfPicked: a provider without a model or a model without a provider,
// which joinModel would silently drop; the caller says so instead.
export const halfPicked = (p) => (p.provider && !p.model.trim()) || (!p.provider && p.model.trim());
// agentModelRE mirrors the server's rule in server.go: blank, or provider/model (C-1, H-8).
const agentModelRE = /^[a-z0-9._-]+\/\S+$/;
export const badModel = (v) => v && (v.length > 128 || !agentModelRE.test(v));
