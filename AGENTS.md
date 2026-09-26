# Working in this repo

Operational notes for an agent, not user docs — those live in
[README.md](README.md) and [docs/](docs/README.md). Add here only things
that would otherwise be rediscovered the hard way.

## lab/

- `make up` calls `lab/vms.sh`, which only starts VMs it creates. A VM
  left over stopped from a prior session (`qm list` shows `stopped`) is
  treated as "exists" and never started — `make up` then hangs forever
  waiting for SSH on a machine that never boots. Fix: `qm start <id>` for
  each stopped guest on Proxmox first (see `lab/lib.sh`'s `pve()`), then
  re-run `make up` or just wait for the guest-agent install step.
- A remote enrolled before jobs ran as root still has the nftables
  cage, `homedash-sudo` and the agent account in the docker group;
  `safety_test.sh` fails on it until the card's Re-provision runs.
- A remote enrolled before the remote model was set in Settings
  won't have `.omp/agent/models.yml` or `config.yml` on disk — the fleet
  default is applied at enrollment, not retroactively. Backfill with
  `POST /api/hosts/{host}/credentials` (writes `models.yml`) then
  `PUT /api/hosts/{host}/agent {"model":""}` (writes `config.yml` from
  the current default).
- The lab's two houses are already peered in a space (`space.name` in
  Settings), with a live storage cluster named `pool` spanning
  remote-big's two extra disks and remote-mid's one, and a `whoami`
  (traefik/whoami) stack on remote-mid:8000. There are no spare disks or
  a third house in `lab/lab.env`'s VM table — reuse these instead of
  building throwaway ones.
- Two connection behaviors are real, not bugs: a fresh cross-house
  service link's first dial can 502 (the lab talks over a relay, not a
  hole-punched connection — see lab/README.md's Limits), and a peer's
  offer refreshes on a roughly 60s cadence, so it can briefly not list a
  model the peer actually holds right after it (re)connects. Retry past
  these before treating a miss as real.

## tests/

- `./tests/run.sh` runs the acceptance suite against the live lab; see
  [tests/README.md](tests/README.md).
- Bash gotcha: a `for x in ...` loop variable in a sourced test file is
  NOT implicitly local. `run.sh` keeps its own CLI-argument filter in a
  global array; a test file that loops over a same-named variable
  without `local` silently corrupts it and breaks later files in the
  run. Always `local` every variable a test function sets, loop
  variables included.
- `sudo -u homedash homedash token NAME` revokes any existing token of
  that name and mints a new one — a name is one live token
  (`POST /api/tokens` refuses a duplicate name with 409). `tests/lib.sh`
  uses a fixed name (`tests-suite`) so repeated calls replace the same
  row instead of piling up; `run.sh`'s cleanup deletes it by name once
  the suite finishes.
