// The lab from a test's point of view: an SSH tunnel to the hub's panel,
// and the hub's shell for the two things only a shell may do — print a
// registration code and mint an API token.
import { spawn, execFile } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const SSH = path.join(here, '..', '..', 'lab', 'ssh.sh');

export const HUB = process.env.UI_HUB || 'hub-a';
// Not configurable: passkeys are bound to the origin the hub registers
// them for, http://localhost:7433, so that is where the tunnel must land.
export const PORT = 7433;
export const BASE = `http://localhost:${PORT}`;

// hubSh runs one command on the hub over the lab's jump, like tests/lib.sh
// does for the API suites, and returns its stdout.
export function hubSh(cmd) {
  return new Promise((resolve, reject) => {
    execFile(SSH, [HUB, cmd], { timeout: 60_000 }, (err, stdout, stderr) => {
      if (err) reject(new Error(`${HUB}: ${cmd}: ${stderr || err.message}`));
      else resolve(stdout);
    });
  });
}

// invite prints a one-time registration code from the hub's shell — the
// authority docs/running/accounts.md says outranks the panel.
export async function invite(role = 'admin') {
  const out = await hubSh(`sudo -u homedash homedash invite ${role}`);
  const m = out.match(/^\s+(\S+)\s*$/m);
  if (!m) throw new Error(`no code in: ${out}`);
  return m[1];
}

// token mints an admin API token named NAME, for a test's setup and
// cleanup outside the browser.
export async function token(name) {
  return (await hubSh(`sudo -u homedash homedash token ${name}`)).trim();
}

// api calls the hub through the tunnel with a bearer token; returns
// {status, body} with body parsed when it is JSON.
export async function api(tok, method, p, body) {
  const r = await fetch(BASE + p, {
    method,
    headers: { Authorization: `Bearer ${tok}`, 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await r.text();
  let parsed = text;
  try { parsed = JSON.parse(text); } catch {}
  return { status: r.status, body: parsed };
}

async function healthy() {
  try {
    const r = await fetch(`${BASE}/api/health`, { signal: AbortSignal.timeout(1000) });
    return r.ok;
  } catch { return false; }
}

// globalSetup opens `ssh -L 7433:localhost:7433` to the hub unless
// something already answers on that port (a tunnel you opened by hand),
// and returns the teardown that closes it. The child is killed by pid:
// ssh.sh execs ssh, so the pid is the tunnel itself.
export default async function globalSetup() {
  if (await healthy()) {
    console.log(`using the tunnel already on ${BASE}`);
    return async () => {};
  }
  const child = spawn(SSH, [HUB, '-N', '-L', `${PORT}:localhost:7433`], { stdio: ['ignore', 'ignore', 'pipe'] });
  let err = '';
  child.stderr.on('data', (d) => { err += d; });
  const deadline = Date.now() + 60_000;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) throw new Error(`tunnel to ${HUB} exited: ${err}`);
    if (await healthy()) {
      console.log(`tunnel to ${HUB} on ${BASE} (pid ${child.pid})`);
      return async () => { child.kill(); };
    }
    await new Promise((r) => setTimeout(r, 500));
  }
  child.kill();
  throw new Error(`no answer from ${HUB} through the tunnel within 60s: ${err}`);
}
