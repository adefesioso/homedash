# Two networks

`space.network` picks which DHT — [the choice](../../../docs/sharing/network.md).

- **public** (default): client mode on the IPFS DHT through its bootstrap
  nodes. The hub declares itself private up front and reserves slots on
  two relay-v2 peers it finds there, so it is reachable before the hole
  punch lands.
- **private**: the DHT protocol is prefixed `/homedash` so it never
  meets the IPFS one, and the bootstrap peers are the multiaddrs in
  `space.bootstrap`. A hub with `space.reachable` on runs in server mode,
  listens on `space.port`, declares itself public and runs the relay-v2
  hop service for the others; a hub without it is a client and relays
  through the reachable hubs. `space.psk` (64 hex) is a libp2p private
  network key: the swarm handshake fails without it, and the host listens
  on TCP only, since QUIC cannot carry a PSK.

`space.reachable` and `space.port` apply in both modes.

A hub found at the rendezvous is protected from the connection manager
and redialed as soon as its last connection drops (backing off from a
second to half a minute until it is back or the node leaves), because a
public relay resets every relayed connection after two minutes or
128 KB and the minute-long discovery tick alone would leave the peer
disconnected for most of that.
