export function bytes(n) {
  if (n == null) return '—';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return `${n < 10 ? n.toFixed(1) : Math.round(n)} ${u[i]}`;
}

export function ago(iso) {
  if (!iso) return 'never';
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 90) return `${Math.round(s)}s ago`;
  if (s < 5400) return `${Math.round(s / 60)}m ago`;
  if (s < 172800) return `${Math.round(s / 3600)}h ago`;
  return `${Math.round(s / 86400)}d ago`;
}

export function when(iso) {
  return iso ? new Date(iso).toLocaleString() : '';
}

// A token count, compacted at the thousand: 950, 12.3K, 1.2M.
export function count(n) {
  if (n == null) return '—';
  if (n < 1000) return `${n}`;
  if (n < 1e6) return `${(n / 1e3).toFixed(n < 10e3 ? 1 : 0)}K`;
  return `${(n / 1e6).toFixed(n < 10e6 ? 1 : 0)}M`;
}

// A dollar cost, three places under a cent, two above.
export function usd(n) {
  if (n == null) return '—';
  return `$${n < 0.01 && n > 0 ? n.toFixed(4) : n.toFixed(2)}`;
}

// A sparkline path for an SVG viewBox of w×h from a series of numbers.
export function sparkPath(values, w = 80, h = 20) {
  const v = values.filter((x) => Number.isFinite(x));
  if (v.length < 2) return '';
  const min = Math.min(...v), max = Math.max(...v), span = max - min || 1;
  return v.map((y, i) => `${i === 0 ? 'M' : 'L'}${(i / (v.length - 1)) * w},${h - ((y - min) / span) * (h - 2) - 1}`).join(' ');
}
