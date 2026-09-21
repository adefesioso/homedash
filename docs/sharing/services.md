# Publishing a service

*The thing you want from outside is almost never a model. It's the media
server, the photo library, the wiki — reachable from your own phone on the
train, from a friend's house, or by a handful of people who don't run a
lab and never will. A browser can't join a DHT.*

A **service** is a name for one port on one machine you own — a stack's
port from the Apps tab, or any port from a host card — and the list of
peers that may front it. Nothing changes on the machine: a request for
the service arrives at your hub over the space connection and your hub
copies it to the port through the SSH connection it already holds, the
same way it copies everything else. A service stays reachable through
exactly the peers it was published to, and unpublishing it makes it
disappear from their next offer.

Two ports are never publishable: 22, because it's SSH, and the hub's own
port, because a service could otherwise front the hub itself — the hub's
and SSH's ports are never published, whatever machine is named. Publishing
again under a name already in use is refused (409) unless the request
carries `replace: true`; the Publish form offers a replace checkbox once
it sees that refusal, so replacing one is still one click, never a silent
swap of the port behind a peer's back.

A peer that was named can front the service two ways, and it chooses:

- **As a link.** The service appears in that peer's panel, under its own
  address, behind its own sign-in — so a HomeDash on your laptop is your
  lab from any network, and a household in your space reads your wiki as
  a page on their panel. A service fronted under a path needs an app that
  can live under one; most can, and say where.
- **As an ingress.** A peer with a public address and a hostname pointed
  at it terminates HTTPS for that name and copies bytes to your hub over
  the connection it already holds. Anyone with a browser reaches your
  service at that name. Certificates are the ingress's business and
  automatic; nothing is asked of you but the name.

The second is the one that makes the space more than the sum of its
labs: one member with a public address is a door for every member behind
a NAT they can't open, and it costs that member a hostname.

Fronting is not a right. A named peer still approves the service in its
own Peers tab before serving a byte of it, and picks the hostname if it
is an ingress; so putting a service on the public internet takes two
people's acts, and either can undo it alone. HomeDash puts no login of
its own in front of an ingress: whoever arrives meets the app's own
sign-in, and a service with none is public to everyone who can reach its
front. Publish accordingly.

It stays inside every rule above:

- **One hop.** The front is the sender, your hub is the origin, and the
  origin copies to its own machine or refuses. A service names only a
  port on a machine the hub owns, so a hub cannot publish something it is
  itself reaching through a peer, and nothing can be re-exported.
- **Named, not discovered.** A service is offered only to the peers it
  was published to; the rest of the space never learns it exists.
- **Both sides decide.** Approval and the hostname sit on the fronting
  side; the origin refuses a peer it has not approved, a service it did
  not publish to that peer, and connections past its own ceiling or past
  that peer's share of it, and copies no faster than the bandwidth it
  set aside per peer.
