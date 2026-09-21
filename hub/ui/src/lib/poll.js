// poll runs fn now and every ms after, for as long as the caller's
// effect lives — and not while the tab is hidden: a panel left open in a
// background tab would otherwise keep every remote's numbers flowing to
// nobody. Coming back runs fn at once, so what is shown is current
// again before the next tick. Returns the stop function an $effect
// returns.
export function poll(fn, ms) {
  let timer = null;
  const start = () => { if (timer == null) { fn(); timer = setInterval(fn, ms); } };
  const stop = () => { if (timer != null) { clearInterval(timer); timer = null; } };
  const onVisibility = () => { if (document.visibilityState === 'hidden') stop(); else start(); };
  document.addEventListener('visibilitychange', onVisibility);
  if (document.visibilityState !== 'hidden') start();
  return () => { stop(); document.removeEventListener('visibilitychange', onVisibility); };
}
