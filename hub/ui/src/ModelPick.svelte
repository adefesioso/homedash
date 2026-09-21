<script>
  // The one provider-and-model picker: a provider chosen from what the
  // vault can run (GET /agents/models), a model id typed or picked beside
  // it. Settings uses it for the fleet defaults, a host card for its
  // override, so a model is chosen the same way everywhere. `pick` is
  // {provider, model}; the caller joins it as "provider/model", since an
  // id alone does not say who runs it (openai/gpt-4o-mini is OpenRouter's
  // tag for an OpenAI model).
  let { pick = $bindable(), providers = [], blank = 'no model', listId, disabled = false } = $props();
  // The list from omp plus whatever is already set, so a saved provider
  // the vault no longer holds still shows (and can be changed).
  const providerNames = $derived([...new Set([...providers.map((p) => p.name), pick.provider].filter(Boolean))].sort());
  const models = $derived(providers.find((p) => p.name === pick.provider)?.models || []);
</script>

<span class="pick">
  <select bind:value={pick.provider} {disabled}>
    <option value="">{blank}</option>
    {#each providerNames as name}<option value={name}>{name}</option>{/each}
  </select>
  <input list={listId} placeholder={pick.provider ? `model id, e.g. ${models[0] || 'gpt-4o-mini'}` : 'pick a provider first'} disabled={disabled || !pick.provider} bind:value={pick.model} />
  <datalist id={listId}>{#each models as m}<option value={m}></option>{/each}</datalist>
</span>

<style>
  .pick { display: grid; grid-template-columns: minmax(8rem, 1fr) 2fr; gap: 0.4rem; }
  .pick select, .pick input { min-width: 0; }
</style>
