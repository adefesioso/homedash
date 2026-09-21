<script>
  // An empty tab says what is missing in one line and offers the one
  // thing to do about it; the how is on the control, not in a paragraph.
  let { text, action, onaction } = $props();
</script>

<div class="empty dashed">
  <p>{text}</p>
  {#if action}<button class="primary" onclick={onaction}>{action}</button>{/if}
</div>

<style>
  .empty {
    display: grid; gap: 0.9rem; justify-items: center; text-align: center;
    color: var(--muted);
    padding: 3rem 1.5rem; max-width: 36rem; margin: 2rem auto;
  }
  p { margin: 0; max-width: 40ch; }
</style>
