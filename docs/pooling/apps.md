# Apps

*Services end up scattered across machines, installed by hand in whatever
way seemed right that evening, with no list anywhere.*

**Apps are compose stacks. Nothing else.** The Apps tab asks every remote
what stacks it has and what their containers are doing, live, on each
open — nothing about an installed app is stored on the hub, so there's no
state to drift. A remote that doesn't answer is named at the top of the
tab rather than just leaving its stacks off the list, the same way an
offline host's rows are marked on [Storage](storage.md) and
[Network](network.md).

You can install or update a stack, read its compose file and recent
logs, start, stop, restart or pull it, and remove it with or without its
volumes. **Deploying a name already running on a host replaces it** —
it's the same call as Update, so the form warns you when the name
clashes before you press Deploy. **Pull** pulls the images and then
restarts the stack on them (two steps; a pull with nothing changed still
restarts). **A failed deploy is removed**, not left half up: a
non-zero `up -d` is followed by tearing the project back down on that
host, and the original error is what you see.

**The Catalog tab** installs a stack without you writing YAML for it
twice — save a compose file once as a catalog entry, with the volumes it
wants and a rough requirements line, and install it again later with one
click; installing hands the entry to the Apps tab, which runs it through
placement the same as a pasted file. Nothing about a catalog entry is
privileged: what lands on the remote is a compose file indistinguishable
from one you pasted in yourself. The catalog ships empty; every entry in
it is one you added, and you can edit or delete any of them — an edit
opens the entry in the same form it was saved from and saves back over
it, under the same name.

The catalog is also the [hub agent](agents/README.md)'s recipe book.
Asked to install something, it reads the catalog first and deploys the
entry that fits, with its compose file as written, before it writes YAML
of its own; and a stack it installed that is not in the catalog yet it
can save there — the compose file read back from the remote, the one
actually running, not a draft — so the next install of the same thing
starts from what already worked. It saves the compose file only, never a
stack's `.env`.

**What a compose file may ask for is gated for an agent.** A job token, a
session on the Agents tab or an outside assistant through MCP deploying a
stack meets the same danger list [`homedash-sudo`](../running/safety.md)
already refuses on a docker command line, read from the compose file
instead: `privileged: true`, `pid: host`, `network_mode: host`, a
`cap_add` of `SYS_ADMIN` or `ALL`, or a bind mount of `/` or the docker
socket. Any of these is refused before the file ever reaches a remote. A
person deploying from the panel or the CLI is not an agent and is not
stopped — the same request goes through, with a warning line in the
response naming what would have been refused — because a person reading
the compose file they just pasted in is the check; an agent acting on
its own is not.

**Publish** on a stack's port makes that service reachable from outside
the house through the peers you name — see
[publishing a service](../sharing/services.md). It changes nothing on the
machine: the hub reaches the port through the SSH connection it already
holds, so a locked machine stays locked and a service is published the
way the lab already works.

## Where a new app should go

*You know what you want — "put this somewhere with room for the photos" —
not which machine that is.*

Installing anything runs it through **placement** first: a scoring
function that weighs what the app needs against every remote's cores,
available memory, free disk and disk *trend*, and ranks the fleet with a
sentence per verdict — a candidate has to leave margin, not just fit
exactly. A stack that wants a [storage cluster](storage.md) belongs on
that cluster's gateway, and placement knows it.

The trend is a week's worth of free-space samples, and a single burst —
a big model pull, a big delete — is dropped before it's read as a slope:
one sample swinging by more than half the week's net change is a step,
not a trend. The estimate never claims more than the window of samples
behind it, and never says "0 days" — a disk already past the line reads
as **would be full today**.

The hub proposes; you can overrule it. This is arithmetic, not a model —
no key, no account, no inference involved — so it works on a fleet with no
GPU in it and it gives the same answer twice.
