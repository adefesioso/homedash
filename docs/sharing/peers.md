# What a peer decides

## Approval and quotas

Because every job or connection arrives with a verified key rather than a
shared secret, a hub can meter senders individually.

| Control | Meaning |
| --- | --- |
| Approved | Whether this key may send jobs or connections to this hub — and whether this hub will send jobs to it. Trust is one switch, both ways |
| Max concurrent | How many of your machines it may occupy at once, under a hub-wide ceiling. 0 means none — approved but never actually served |
| Per hour | Jobs accepted from it in a rolling hour. 0 means none, the same as above |
| Fronts | Which of this key's published services this hub serves, and as a link or under which hostname |
| Connections | Hub-wide, in Settings: how many connections for published services this hub copies at once, across every peer fronting them; and how many any one peer may hold |
| Bandwidth | Hub-wide, in Settings: the most bytes per second copied for any one peer's fronting, in either direction; blank is unlimited |
| Jobs per hour | Hub-wide, in Settings: jobs accepted from the whole space in a rolling hour, whatever each peer's own allowance adds up to |
| Unknown peers | Space-wide default for a key not yet in the list: refuse, or accept under a default quota |

A key is free to make, so a hub that accepts unknown peers is accepting
as many keys as anyone cares to generate; the hub-wide ceilings — jobs at
once, jobs per hour, connections — are what bound that, and they apply
whether the senders are one hub or fifty. Approving a peer means the
key, not the name: two hubs can call themselves the same thing, and the
Peers tab shows the key under the name so approval can go by it.

The **Peers tab** is where that list lives: every hub discovered in your
space, with the controls above per row and a count of what each has been
served. **Discovery is automatic and fills the list; approval is a
person's act.** Approval is also what lets this hub *send* to a peer:
a prompt goes only to a key someone here has approved, never to a hub
known only by its offer, so the first thing a stranger's hub reads is
never yours. Whether this hub runs peers' jobs at all is a switch of
its own, off by default. The tab also lists the services this hub has
published and to whom.

## Keeping the score

*A space runs on reciprocity and neither side can see any.*

Each peer row counts both directions — jobs sent to that key and jobs
served for it, by model, and bytes carried for that key's services and
bytes it carried for yours, over the last day, the last week and all time
— with the two totals at the foot of the Peers tab: what this hub has
taken from its space, and what it has given back.

That is a number, not a currency. Nothing in the protocol enforces balance
and nothing on the wire carries a score; a hub that only ever borrows is
dealt with by whoever tires of it first, through the approval and quota
controls that already exist. The count is there so that decision rests on
evidence rather than a feeling, and so that a hub quietly carrying its
space can tell that it is.

The counts and the [record](jobs.md#choosing-a-peer) are different
things: counts are for a person deciding whether an exchange is fair, the
record is for the router deciding where the next job goes. A peer can be
slow and generous at once, and neither number should be allowed to argue
the other's case.

## What this costs you in privacy

Worth stating plainly, because encryption invites the wrong assumption:
the prompt is encrypted **from A to B**, not from B. B has to read it to
run it, so its operator can see what you sent. Sharing a space means
trusting its members with the text of your prompts, exactly as much as
sending them a message would. The encryption protects the journey, not
the destination. Approve peers accordingly, and keep anything you wouldn't
show them on machines you own.

The same is true the other way round for a service. A front terminates
the visitor's connection — an ingress holds the certificate for the
hostname — so its operator can read everything that passes between your
service and its visitors, passwords included, exactly as your own reverse
proxy could. Fronting is a job you give to one peer, and it should be a
peer you'd let stand between you and your users.

Only what you named crosses. An [agent job](../pooling/agents/README.md),
the credentials behind it, the logs it leaves and every port you didn't
publish are never offered to a space.

What B may *not* do is send it on. A job from a peer is served on B's own
machines or refused, and a connection for a service is copied to the one
port it names or refused — never forwarded to another hub. So the set of
people who can read your prompt, or your users' traffic, is the one hub
you chose, and it cannot grow after you chose it.
