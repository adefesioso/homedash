# What a remote can be rebuilt from

*A remote is the sum of everything ever done to it — by hand, from the
panel, by its agent — and when its disk dies the only record of that is
your memory.*

Every job ends with a report, and the report says what changed. The hub's
agent folds each one into that remote's **rebuild script**: one shell
script per host, kept on the hub in the host's record, that takes a fresh
Debian to the state this remote is in now — packages installed, files
written, services configured, mounts made. Keeping it current is the hub
agent's standing duty, not a step you ask for: a job whose report the
script doesn't yet reflect is a job that isn't finished. What the panel
itself does to a remote — a stack deployed, Ollama installed, a model
pulled, a cluster mounted, the agent's model set — the hub appends on its
own, because it knows exactly what it ran.

The script is readable and editable on the host card, and it is a record,
not a state: the hub never runs it against a machine to make the two
agree, and the machine's word still wins on the panel. It contains what
was reported, so a change made at the console that no job was told about
isn't in it.

**It carries the small data too.** This is the one exception to "setup,
not data": a rebuilt machine with the right packages and none of its
files is not the machine back. Small things — config files, keys the job
made, compose files, a database dump the job was asked to take — go into
the script inline, as heredocs. Anything larger the script cannot carry:
it names those paths at the top, under what it can't recreate, so the
list of things that need a [backup task](../tasks.md) of their own is
written down rather than silently left out.

**New remote** can carry one. Pick a host — one that is failing, or gone —
and the enrollment of the new machine runs that host's script after the
base setup, so a dead box is recreated rather than remembered. Along with
the pinned host key, the script is the thing a
[backup](../../running/README.md#where-the-state-lives) keeps per host
that you would otherwise be doing again by hand.
