// readable turns a failed response's body into something worth showing:
// an HTML error page (a proxy's 502, the panel's own SPA shell on a stale
// route) becomes one plain sentence instead of markup in the error span.
function readable(text, status, path) {
  const t = text.trim();
  if (t.startsWith('<')) return `the hub answered ${status}`;
  return t || `${path}: ${status}`;
}

async function call(method, path, body) {
  const r = await fetch(`/api${path}`, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : {},
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (r.status === 401) {
    dispatchEvent(new CustomEvent('homedash:signed-out'));
    throw new Error('sign in');
  }
  if (!r.ok) throw new Error(readable(await r.text(), r.status, path));
  if (r.status === 204 || r.status === 202 || r.headers.get('content-length') === '0') return null;
  const ct = r.headers.get('content-type') || '';
  if (!ct.includes('application/json')) throw new Error(`${path}: unexpected ${ct || 'reply'} (${r.status})`);
  return r.json();
}

export const get = (path) => call('GET', path);
export const post = (path, body) => call('POST', path, body);
export const put = (path, body) => call('PUT', path, body);
export const del = (path) => call('DELETE', path);
