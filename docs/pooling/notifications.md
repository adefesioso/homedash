# Notifications

*Everything the panel records is a transition, and none of it reaches
you. A machine off since Tuesday, a mountpoint at 98% — all of it waits on
a page you have no reason to open, which is how a lab drifts even with a
panel in front of it.*

Events record transitions rather than states, which is exactly what makes
them worth sending somewhere. A **notification target** — an
[ntfy](https://ntfy.sh) topic, or any URL to POST at — receives them as
they land: a host going Offline and coming back, a key mismatch, a cluster
gone Degraded, a mountpoint crossing a fullness threshold you set, a task
that has started failing and one succeeding again, an agent job that
failed or is marked needs you, a remote whose credential pull failed, a
peer's job refused for a reason you would have wanted to fix, a restore
applied from an export, and a session a hub restart ended
(`session.ended`, reason "hub restarted") — reattach is out of scope, so
this is the only trace a live session leaves.

There is no per-event picker. Every one of these is a transition into or
out of trouble, so the volume is low by construction, and a list of
toggles would mostly be a way to switch off the one that mattered. Sending
is best-effort and never blocks what it reports on.
