# Keeping it updated

*A hub is the machine nobody logs into. If a new HomeDash only arrives
when you remember to download a `.deb`, it doesn't arrive.*

Releases are published as `.deb` files on
[the releases page](https://github.com/adefesioso/homedash/releases) —
one per architecture, `amd64` and `arm64`. A releases page is not an apt
repository: it has no package index, so `apt` cannot read it, and the
box you installed on will sit on the version you gave it forever.

## What the script does

[`hub/packaging/apt-repo.sh`](../../hub/packaging/apt-repo.sh) puts an
index in front of the releases page, on one machine, so HomeDash upgrades
with everything else on it. Run it once as root on any box that has the
package installed:

```
sudo ./apt-repo.sh
```

It leaves four things behind:

- `/var/lib/homedash-apt` — a one-package apt repository on local disk,
  holding the newest `.deb` for this machine's architecture and the
  `Packages` index `apt` reads.
- `/usr/local/bin/homedash-apt-sync` — asks GitHub for the latest
  release, downloads the `.deb` matching `dpkg --print-architecture` if
  it isn't already there, drops older ones, and rebuilds the index.
- `/etc/apt/sources.list.d/homedash.sources` — points `apt` at that
  directory.
- `/etc/apt/apt.conf.d/99homedash-apt-sync` — runs the sync on every
  `apt update`.

From then on `sudo apt update && sudo apt upgrade` offers the new
HomeDash beside the Debian ones, and installing it is an ordinary package
upgrade: the service stops, the binary is replaced, the service starts,
and `omp` is re-fetched at the version the new hub wants. `apt-daily`
already runs `apt update` on a timer, so even a bare `sudo apt upgrade`
is at most a day behind.

`sudo ./apt-repo.sh --uninstall` removes all four and leaves the
installed package alone.

## What it does not do

**`apt upgrade` on its own does not reach the network.** `apt` has no
hook that runs before `upgrade` resolves versions — only before
`update` — so the sync is tied to `apt update`. This is how every
third-party repository behaves, not a limitation of the script.

**The repository is unsigned.** It is a directory on your own disk that
only root can write, so the source is marked trusted rather than checked
against a key; what is actually trusted is the HTTPS connection to
GitHub that fetched the `.deb`. Signing would mean publishing a real
repository with a key, which is a change to
[the release workflow](../../hub/README.md#building), not to one machine.

**It is per-machine.** Each box you want on this path runs the script
once.
