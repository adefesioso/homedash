# tests

*The README describes what the hub does; nothing here checks that a live
fleet actually does it.*

An acceptance suite that runs the claims in [../README.md](../README.md)
and [../docs](../docs) against a real [lab](../lab/README.md) — real SSH,
a real gate, a real Ollama pool, two real houses on the DHT — instead of
a Go unit test's fakes.

## Use

```
../lab/make up       # if the lab isn't already running
../lab/make deploy   # put the current build on both hubs
./run.sh             # every test file
./run.sh safety tasks # just tests/safety_test.sh and tests/tasks_test.sh
```

Each `*_test.sh` maps to one doc file and calls hub-a's and hub-b's own
APIs the way the panel, a window or the CLI would — run
*from* the hub over the lab's SSH jump, since the houses are deliberately
unreachable from this machine directly (one of the claims under test).
Tests mint their own admin tokens and clean up what they create; the
storage and sharing suites reuse the lab's existing "pool" cluster and
peer connection rather than building throwaway ones, since the lab's VM
table has no spare disks or third house to spare.

The API suites never see the panel. [ui/](ui/README.md) drives the real
panel in a real browser — passkey sign-in included, with a software
authenticator instead of a switch that turns the login off — and runs
from `./run.sh` as `ui`, or alone with `cd ui && npx playwright test`.

## What is and isn't covered

Every doc file has a suite, but a suite is not the whole doc.
[COVERAGE.md](COVERAGE.md) lists, per doc, which claims a test runs and
which are still untested and why — the lab has no third house, no
btrfs root, no offline remote — so a gap is written down rather than
mistaken for a pass.

## Files

| File | Claims from |
| --- | --- |
| `lib.sh` | shared `hub_curl`/assert helpers, not a doc |
| `hosts_test.sh` | [docs/pooling/hosts.md](../docs/pooling/hosts.md) |
| `network_test.sh` | [docs/pooling/network.md](../docs/pooling/network.md) |
| `apps_test.sh` | [docs/pooling/apps.md](../docs/pooling/apps.md) |
| `tasks_test.sh` | [docs/pooling/tasks.md](../docs/pooling/tasks.md) |
| `storage_test.sh` | [docs/pooling/storage.md](../docs/pooling/storage.md), the cluster half |
| `workspaces_test.sh` | [docs/pooling/storage.md](../docs/pooling/storage.md#a-shared-workspace), the workspace half |
| `inference_test.sh` | [docs/pooling/inference.md](../docs/pooling/inference.md) |
| `agents_test.sh` | [docs/pooling/agents/](../docs/pooling/agents/README.md): jobs, rounds, rollback, credentials, secrets, rebuild |
| `notifications_test.sh` | [docs/pooling/notifications.md](../docs/pooling/notifications.md) |
| `sharing_test.sh` | [docs/sharing/](../docs/sharing/README.md) |
| `backup_test.sh` | [docs/running/README.md](../docs/running/README.md#where-the-state-lives), export and restore |
| `health_test.sh` | [docs/running/README.md](../docs/running/README.md#is-the-hub-well), the hub's own health |
| `accounts_test.sh` | [docs/running/accounts.md](../docs/running/accounts.md) |
| `cli_test.sh` | [docs/running/cli.md](../docs/running/cli.md): the installed binary's subcommands, with a token; `login` itself is in [ui/](ui/README.md) |
| `safety_test.sh` | [docs/running/safety.md](../docs/running/safety.md) |
| `ui_test.sh` | runs [ui/](ui/README.md), the browser suite — sign-in and the CLI's browser hand-off included — as one test |
