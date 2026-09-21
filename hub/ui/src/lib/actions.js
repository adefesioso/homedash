// useEscape: a Svelte action for the element that wraps an inline
// "dialog" — a card that opens over a tab (New remote, Install a stack,
// the New task form, an attached window…), never a real <dialog>. There
// is no single place these all live, so instead of App.svelte reaching
// into every tab's state, each dialog's own element carries this action
// with the callback that closes it (X-11).
//
// It listens on window rather than the node itself: focus often never
// moves into the card (a click opens it and keeps focus on the button
// that did), so a listener on the node alone would miss Escape most of
// the time. Because the action's lifetime is the node's — mounted when
// the {#if} opens the card, destroyed when it closes — there is never
// more than one listener per open dialog, and it is gone the moment the
// card is.
export function useEscape(node, onEscape) {
  let cb = onEscape;
  function onKey(e) {
    if (e.key === 'Escape') { e.stopPropagation(); cb?.(); }
  }
  window.addEventListener('keydown', onKey);
  return {
    update(next) { cb = next; },
    destroy() { window.removeEventListener('keydown', onKey); },
  };
}
