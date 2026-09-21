# Installing on Debian

HomeDash is one `.deb`, one binary, and it installs as three things:

- **The hub**, a system service that starts at boot and runs
  everything in [running it](README.md). Closing every window changes
  nothing about what the lab is doing.
- **The panel**, a desktop entry in your applications menu that opens
  the hub's page on this machine. It opens in your browser as an
  app-style window rather than in an embedded web view, because passkeys
  have to work and the browser is where they do.
  The panel is titled with the hub's hostname — the machine's own name,
  nothing to configure — so two hubs open side by side (or through
  tunnels that both land on `localhost`) are never mistaken for one
  another.
- **The CLI**, the same `homedash` binary on any machine, signed in to
  a hub with your passkey — [working it from the command line](cli.md).
  A workstation installs the same package and never starts the service.

`apt install ./homedash_*.deb` on the box you've picked as the hub;
open HomeDash from the menu. To open it from any other machine in the
house, run `hub/packaging/https.sh` once on the hub: passkeys only work
over HTTPS off `localhost`, and it puts the panel behind
`https://homedash.local` with a certificate your devices trust once —
[opening the panel from other machines](https.md). Beyond that, nothing
else to install and nothing to configure — the first start creates the state file and the hub's key, and
fetches the one dependency the package does not carry: `omp`, at the
version this hub was tested against, into the hub's own state directory.
The hub drives `omp` over a protocol, so the version is the hub's to
choose, not the system's; it is updated with the hub and never by hand.
Both `amd64` and `arm64` builds exist.

Uninstalling removes the service, the binary and the hub's copy of `omp`,
and leaves the state directory where it was, because that is the part you
can't rebuild.

A releases page is not an apt repository, so an installed hub stays on the
version you gave it until you hand it another one — unless you
[point apt at the releases](updating.md).
