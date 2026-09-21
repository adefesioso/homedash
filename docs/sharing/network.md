# The network under a space

*A space needs some way for two hubs that have never met to find each
other. The public DHT does that for free and for anyone; the price is that
"anyone" includes people who are not in your space. A private DHT costs
one member a public address, and buys back everything the public one
gives away.*

## Public: the IPFS DHT

The default. A hub joins the public Kademlia DHT through the IPFS
bootstrap nodes, advertises the hash of the space name, and looks up
everyone else who advertised the same hash. Nobody runs anything.

What it gives away, stated once:

- **The space name is a password.** The rendezvous is a hash of the name,
  and hashing is not hiding: anyone who guesses or is told the name finds
  every hub in the space. Pick a name the way you'd pick a passphrase —
  `elm-street-labs-7f3k9q`, not `family` — and treat it like one.
- **Membership is visible.** The DHT nodes nearest the hash — including
  whoever chooses to run one there — hold the list of hubs in the space,
  their relay addresses, and when they come and go.
- **The hub is fingerprintable.** Every node the hub connects to learns it
  speaks the HomeDash protocols, whether or not it knows the space name.
  That is why an offer from a node the hub did not find at the rendezvous
  is dropped unread.
- **Discovery and relaying run on someone else's machines.** The IPFS
  bootstrap nodes and volunteer relays see who talks to whom and how
  much, never what; they can throttle, and they can go away. A public
  relay also cuts every relayed connection after two minutes or 128 KB,
  so until the hole punch lands, two hubs behind NATs are reconnected
  through a relay every couple of minutes; the hub redials the moment a
  hub drops, so the gap is a second or two, not the minute the next
  lookup would take.

## Private: a DHT of hubs

Set **Network** to *private* in Settings and the hub speaks the same
protocol to a DHT made only of HomeDash hubs that typed the same
bootstrap addresses. The IPFS nodes are never contacted; the space name
is still the rendezvous, but only hubs in the private network can look
it up.

It needs three things:

1. **One reachable hub.** At least one member has a public address (or a
   port forwarded on its router) and sets *This hub is reachable from the
   internet* with a fixed *Listen port*. That hub serves the DHT and
   relays for the members behind NAT. Two reachable hubs are better than
   one; every hub may be one.
2. **Its addresses, copied once.** The reachable hub's Peers tab lists
   its addresses; the others paste them into *Bootstrap hubs*. A hub
   behind NAT needs no port and pastes only.
3. **Optionally, a network key.** A 64-hex-character secret pasted into
   every member's *Network key* makes the wire itself private: a hub
   without the key cannot complete a handshake, so a leaked space name
   alone admits nobody. Make one with `openssl rand -hex 32` or the
   *generate* button. A keyed network runs on TCP only — the QUIC
   transport cannot carry a private-network handshake — so hole punching
   is TCP simultaneous-open, which lands less often than QUIC's; the
   reachable hub relays for the rest either way.

Everything above the network is the same in both modes: identity is the
connection's key, offers go only to hubs found at the rendezvous,
approval is a person's act, one hop.

## What stays true in both

- **A hub answers nobody it did not find.** The rendezvous is the
  admission list: a node that connects and greets without having been
  found there is ignored, and never enters the Peers tab.
- **Nothing is read before the key is checked.** A greeting or a request
  header is a few kilobytes at most; the body of a job is read only after
  the sender is approved and under quota.
- **A job is a prompt.** The only paths a peer may send are chat,
  generate and embed; anything else Ollama understands — pull, create,
  push, copy — is refused before it reaches a machine.
- **The box has limits.** The hub caps connections and streams per peer
  and in total at numbers sized for one small machine, before any
  HomeDash rule runs.
