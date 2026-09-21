# Credentials and secrets

## One set of credentials

Sign in to a provider once, on the hub — an API key, or the provider's
own login, for which the panel shows the link and takes the code back. The
hub keeps the **vault** and does every refresh. A remote never holds a
refresh token: when it needs a credential it asks the hub for a short-lived
one over the SSH connection the hub opened for the job, and keeps an
encrypted snapshot so a job can start while the hub is busy. Each remote
asks with a token of its own, minted for it and known to no other machine,
so a token lifted from one remote opens nothing on the next. **Update
credentials** on a card, or on every card at once, mints a fresh token and
pulls a fresh snapshot; **Revoke** on a card leaves the remote with a token
the hub no longer answers, until you update it again — the card reads
**credentials revoked** instead of an age until you do; sign out on the
hub and the next pull empties every remote. Nothing about a credential
crosses a space.

A Pi-class box never gets a snapshot — there's no agent there to use one
— so re-provisioning one skips that step instead of failing the whole
run; the response and event say so.

## Secrets for the house

*A job needs a token, a password, a key — and the only places it could
come from are the remote's disk, where it stays forever, or your
clipboard, into the prompt, where it stays in the log.*

The hub is the vault for the whole house, not only for provider logins.
**Secrets** are named values — an API token, a database password, a
Wi-Fi key — kept encrypted on the hub, entered once in Settings and never
shown again. Each is granted to particular hosts, or to every host. While
a job runs, its remote can ask for one by name with `homedash-secret
NAME`, over the same connection the credentials travel on; the answer
comes from the hub, never lands on the remote's disk unless the job puts
it there, and stops being reachable the moment the job ends. Every read
is an event. The hub's agent can list the names a host may ask for — so
it can tell the remote what to use — and can never read a value. Nothing
about a secret crosses a space.
