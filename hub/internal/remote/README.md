# internal/remote

The one door to a remote. Every byte the hub sends to a machine goes
through this package's `Executor`, and the executor does four things
only:

- **Dial** with the hub's key, pinned to the host key enrollment
  recorded: a different key is `ErrKeyMismatch`, refused before
  authentication and never trusted. The pinned key's own algorithm is
  requested so the server presents that key and not another of its keys.
- **Keep** the connection. A handshake is the expensive part of every
  command, and the heartbeat alone runs one per host per minute, so
  `Run` and `Sh` reuse an open client per (address, host key): a
  command failing to open a session drops it, an idle client is closed
  after two minutes, and `Dial` still hands out a fresh, caller-owned
  connection for the callers that hold one across a job. The pin is
  checked on every handshake, kept or not.
- **Run** an argv on a connection. Every word is single-quoted
  (`Quote`), the only quoting that is safe for arbitrary bytes; a script
  travels on stdin (`Sh`), never in the command line. A context ending
  kills the session.
- **Forward** a reverse tunnel: connections to an address on the remote's
  loopback are carried back and dialed here. This is how a remote reaches
  the vault and the job door for exactly as long as the hub holds the
  connection.
- **WriteFile** through the account's own shell — a temp file in the
  target directory, mode set, then an atomic rename — so no scp or sftp
  subsystem is depended on.

A published service's origin side uses the same client's `direct-tcpip`
channel (`ssh.Client.Dial`) to reach a port on the remote's loopback, so
nothing on the remote listens for the hub and a locked host stays
locked. The gate that decides *what* may run is elsewhere
([gate](../gate/README.md), [fleet](../fleet/README.md)); this package
only guarantees *how*.
