# Pooling your own machines

*Nothing connects the machines in a house, so each is an island with its
own SSH session and half-remembered setup ritual. You stop being able to
answer ordinary questions about your own house, and the lab drifts.*

Part one turns every machine you enroll into one fleet you see and act on
from a single page — the **panel** — and gives you one seat to work it
from. Every rule in the [root README](../../README.md) applies: the hub
orchestrates and the remotes do the work, the machine's word wins, and
anything dangerous is refused in code.

## The concepts

- **The hub** is the one small always-on machine running HomeDash. It
  holds the database and the SSH key, and does no work of its own.
- **A remote** is a machine enrolled over SSH, or a phone paired through
  its app — [Mobile](mobile.md).
- **The heartbeat** is the hub connecting to every remote once a minute,
  re-reading what it says about itself, and keeping a few numbers.
- **A device** is anything a remote can see that isn't enrolled — a
  neighbour on the LAN, a Wi-Fi network, a Bluetooth device — known by
  its hardware address and shown as the remotes last saw it.
- **Events** record transitions — into and out of trouble — never states,
  which is what makes them worth sending somewhere.
- **The gate** is the one doorway every remote command goes through, and
  the list of things it refuses.

## What it does

Each item solves one piece of the problem above; its own file has the
detail.

- [Hosts](hosts.md) — enrolling a machine with one pasted line, knowing
  which machines are really there, watching each one's numbers over time,
  locking direct SSH access down to the hub's account, and holding a
  machine at one address so a new lease can't lose it.
- [Network](network.md) — everything else in the house the remotes can
  see — wired and Wi-Fi neighbours, Bluetooth devices — in one list with
  the hub's best guess at what each is, so you can name them and the
  agent knows which remote can reach the television.
- [Apps](apps.md) — every compose stack on every remote, live, installed
  from a catalog or a pasted file, placed by arithmetic on the machine
  that has room, and published to the outside when you say so.
- [Tasks](tasks.md) — every scheduled command in the house in one list,
  run down the SSH path, recorded every time, with no crontab anywhere.
- [Storage](storage.md) — clusters that present several disks as one
  path with their capacities added, and shared workspaces on them that
  several remotes' agents work in at once.
- [Inference](inference.md) — the enrolled GPUs behind one Ollama
  endpoint, the model grid, what fits on each machine, and one placement
  decision per conversation.
- [Agents](agents/README.md) — the seat the fleet is worked from: the
  hub's agent hands jobs to each remote's own agent — unprivileged, caged
  to its own machine, root only through one logged door — reads the
  reports, keeps every host's rebuild script current, and brokers
  credentials and secrets without any of them landing on a remote.
- [Notifications](notifications.md) — the one target every transition
  into or out of trouble is sent to.
- [Mobile](mobile.md) — pairing an Android phone as a remote to puppeteer.
