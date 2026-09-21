# internal/auth

Sign-in: passkeys through `go-webauthn/webauthn`, discoverable, bound to
the RP id and origins the [entry point](../../cmd/homedash/README.md)
configures. Ceremonies in flight are kept in memory for five minutes.

- **Registration.** With no users yet it is open and makes the admin.
  Otherwise an invite code is needed: one naming a user adds a passkey to
  that account (the second passkey the panel keeps asking for), one
  without makes a new account of the invite's role. A signed-in user adds
  a passkey to their own account without a code. The account row is
  created before the ceremony because the passkey is bound to its
  handle; one that never gets a passkey is pruned after five minutes.
- **Login** is discoverable: no username asked; the passkey the browser
  used resolves to its account.

## The account tables

`users` — a random 64-byte handle, name, role (`admin` or `viewer`).
`credentials` — each passkey as `go-webauthn` marshals it, by credential
id. `sessions` — a cookie token good for thirty days. `invites` — role,
an optional user for a second passkey, one use, one day. `tokens` — a
SHA-256 of an API token, a name, a role; the value is shown once. A token
is `Authorization: Bearer` on the API and the MCP server, and the
[guard](../server/README.md#the-guard) treats it as an account of that
role. A session token is accepted as a bearer too — that is what the
CLI sends.

## Signing in the CLI

A terminal can't do WebAuthn, so the [CLI](../cli/README.md) borrows
the browser. `GrantCLI` takes a signed-in session and returns a one-time
grant code, kept in memory for two minutes beside the ceremonies;
`RedeemCLI` spends it for a fresh session of the same account. The code
is what crosses the browser-to-loopback redirect, so the session itself
never appears in a URL or a browser history.
