# tests/ui

*The API suites prove the hub does what the docs say; nothing proves a
person at the panel can reach it — and the first thing in their way is a
passkey prompt no script can click.*

A Playwright suite that drives hub-a's real panel in headless Chromium
over the lab's `ssh -L` tunnel, exactly as `homedash open` would on the
hub itself. There is no test-only way past sign-in: the hub binary is
untouched, and the login screen is under test rather than skipped.

## How the passkey is answered

Chromium exposes a **virtual authenticator** over the DevTools protocol
(`WebAuthn.addVirtualAuthenticator`): a software CTAP2 device with a
resident key that answers `navigator.credentials.create/get` without a
finger on anything. Each test attaches one, registers a fresh account
with a one-time code from `homedash invite admin` on the hub's shell —
the same authority a locked-out admin uses — and signs in with it. The
ceremony reaches the real `/api/auth/*` endpoints and the real WebAuthn
verifier; passkeys are bound to the origin `http://localhost:7433`, so
the tunnel must land on that local port and the suite opens it itself.

## Use

```
npm install                # once; then npx playwright install chromium if none is cached
npx playwright test        # the lab must be up and deployed (../../lab)
npx playwright test --headed   # watch it
```

From the suite's root, `../run.sh ui` runs the same thing as one line in
the summary. `UI_HUB=hub-b` points it at the other house.

## Files

| File | What it does |
| --- | --- |
| `playwright.config.js` | one worker, Chromium, base URL on the tunnel |
| `lab.js` | the tunnel (global setup/teardown), `hubSh` for shell commands on the hub, invite and token minting |
| `fixtures.js` | `hub` (an admin API client on a throwaway token) and `registerAccount` (the register screen, walked) |
| `passkey.js` | attaches the virtual authenticator to a page |
| `accounts.spec.js` | [docs/running/accounts.md](../../docs/running/accounts.md): register with a code, land on the panel, sign out, sign back in |
| `panel.spec.js` | the panel shows what the API reports: it is titled with the hub's hostname, every tab opens, the Hosts tab lists exactly the hosts `GET /api/hosts` returns, and the Health tab draws exactly what `GET /api/hub/health` reports |
| `agents.spec.js` | [docs/pooling/agents/README.md](../../docs/pooling/agents/README.md): a session opens, attaches, and renders `omp` with no uncaught error |
| `cli.spec.js` | [docs/running/cli.md](../../docs/running/cli.md): the real `homedash login` binary (`hub/dist/homedash-linux-<arch>`, or `HOMEDASH_BIN`) with no browser on its PATH, approved from the signed-in panel; the session it saves works and `logout` ends it on the hub; a grant is spent once |

Every account and token the suite makes is named `tests-ui…` and deleted
at the end of its test, so a lab stays as it was found.
