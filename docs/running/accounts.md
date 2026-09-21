# Signing in and roles

## Signing in

*The panel has a shell on every machine in the house behind it, so it
needs a real login. Renting that from an identity provider means the lab
stops working whenever someone else's service does.*

Sign-in is **passkeys**. No identity provider, no password to reuse or
leak, and no second way in to get wrong either. The first passkey
registered becomes the admin; after that, registration is closed and an
admin invites the rest with a single-use code — the same shape enrollment
already uses for machines; an invite can be revoked before it is used.
The panel keeps asking for a second passkey until there are two, because
one passkey on one phone is a lockout waiting to happen; locked out
anyway, a shell on the hub prints a one-time registration code, which is
the only authority that was ever going to outrank the panel.

The [CLI](cli.md) on your workstation signs in with the same passkey,
through the browser, and holds a session like the panel's. Scripts that
can't open a browser use an **API token** instead, made in Settings or
from that same shell, shown once. A token name is unique; revoking it
revokes that token, and only that one.

If you want SSO, put the panel behind the reverse proxy you already run.

## More than one person in the house

Every account has a role, and it is two roles rather than a permission
matrix on purpose:

- **admin** — everything in these documents.
- **viewer** — the panel, read-only. Host cards and their history, apps
  and their logs, storage clusters and disks, peers, the event list, the
  Agents and Jobs tabs as a reader — every session and every job log, without the
  keyboard — the router, and the services peers front as links.

The line that actually exists in a house is between the person who
maintains the lab and the people who live with it. Wanting something finer
than that is usually a sign the lab wants a second hub. The split is
enforced at the API rather than by hiding buttons: the panel greys what a
viewer cannot do; the API still refuses.

Removing your own account is always refused — that's what signing out is
for. Demoting an admin to viewer is refused only when it is the last one:
you may step down yourself if another admin is still standing, but the
house is never left with none.
