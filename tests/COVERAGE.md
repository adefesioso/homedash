# What the suite covers, per doc

Last full run: 2026-09-16 against the lab, every runnable suite green
(`storage` and `workspaces` need the `pool` cluster, which the lab is
missing since its rebuild — see [../lab/README.md](../lab/README.md)).

One row per claim a doc makes. **Runs** means a test in the named file
asserts it against the lab. **Untested** says why: what the lab lacks,
or what would need a deterministic agent. A row moves up when the test
is written; a claim that is not in this file is one nobody has read for.

## docs/pooling/hosts.md — `hosts_test.sh`

| Claim | Runs | Untested because |
| --- | --- | --- |
| New remote mints a single-use code and one pasted line; the script is served to the code and refused to any other | yes | |
| The line enrolls a machine end to end (account, key, Docker, agent, llmfit, specs, host key) | | the lab's remotes are enrolled once by hand; a throwaway VM would be needed to enroll fresh every run |
| A rebuilt or replaced box is refused later as a key mismatch | | needs a remote whose host key changes; no spare VM |
| New remote can rebuild from a named host, and refuses a host that does not exist | yes (up to the code) | the rebuild running on the new machine needs the fresh VM above |
| A Pi-class box enrolls and pools but cannot run the agent | yes | |
| No HomeDash agent or binary on a remote; the cage is a oneshot; nothing of HomeDash's runs or listens there | yes | |
| A joined host starts unknown, then reads Online/Offline/Key mismatch from a real connection | Online only | Offline is exercised in `storage_test.sh` (the VM is stopped there); Key mismatch needs the key change above |
| Each sweep keeps free space per mount, memory, load; metrics are readable | yes | the sparkline is the panel's; `ui/` does not yet open a card |
| The hourly rollup reads only unrolled hours and never rewrites one | unit (`TestRollupBounded`) | |
| Reads go through a read-only pool; commits are `synchronous=NORMAL` | unit (`TestReadOnlyPoolRefusesWrites`) | |
| The lock shuts every login but the hub's; the hub still gets in; the card shows what the machine reports | yes | "lock verifies before applying" (a bad candidate config leaves the machine unchanged) has no way to be provoked from the API |
| Lock and unlock are events and land in the rebuild script | yes | |
| A saved rebuild script round-trips byte-exact (CRLF, a quote, a literal `\n`) | `test_rebuild_script_save_round_trips_byte_exact` (`hosts_test.sh`), `ui/panel.spec.js` "saving a host's rebuild script from the card is byte-exact" | |
| A revoked credential shows on the card, not just in `/api/hosts` | `test_credentials_revoke_shows_on_the_card` | |
| Holding an address is confirmed from the new address; the hub re-pins and the card shows exactly the held address; a bad address or gateway is refused before the machine | `test_holding_an_address_is_confirmed_from_the_new_address` — on netplan (remote-mid), ifupdown (remote-small) and NetworkManager (remote-big) | bare systemd-networkd: exercised by hand on remote-b (house B), which the hosts suite does not cover |
| An address the hub cannot reach is not confirmed; the machine reverts on its own; the refusal is an event; Back to DHCP with no lease reverts to the held address | `test_an_address_the_hub_cannot_reach_reverts_on_its_own`, per manager | a different host key at the new address needs a second machine there |

## docs/pooling/network.md — `network_test.sh`

| Claim | Runs | Untested because |
| --- | --- | --- |
| Scan now records what each online remote can see; an enrolled remote is never a device, and neither is the hub itself | yes | |
| Each remote says which of LAN, Wi-Fi, Bluetooth it can see | yes (absent on all three, as the VMs have neither) | a remote that *can* see Wi-Fi or Bluetooth needs hardware |
| A sighting says which remote saw it and when; a device is its hardware address | yes | |
| Your name and kind sit beside the guess and are never overwritten | yes | |
| The guess: hostname, vendor, kind from what the device advertised | unit (`TestIdentify`) | the lab's LAN has only the VMs and the hub on it |
| `scan_minutes` and `forget_days` are settings; zero switches scanning off | stored and read back | the timer firing, and a device leaving after N days, need a clock |
| A named device survives forgetting and is marked missing | | same |
| The first sighting of a device is an event | | no event kind exists for it in code |
| The agent's `list_devices` and `name_device` tools | | the MCP tools are not driven by this suite |
| Devices never reach a peer | yes | |

## docs/pooling/apps.md — `apps_test.sh`

| Claim | Runs | Untested because |
| --- | --- | --- |
| A catalog entry you add can be installed from and removed again | `test_a_catalog_entry_you_add_can_be_installed_and_removed` | installing *from* a catalog entry goes through the same deploy call as a pasted file |
| Deploy from a pasted compose file; the tab lists it live; deploying is an event | yes | |
| Read the compose file and recent logs | yes | |
| Start, stop, restart, pull; remove with or without volumes | all but pull and remove-keeping-volumes | pull needs a registry round-trip worth waiting for |
| Placement refuses what cannot fit, leaves margin, ranks with a reason, is deterministic | yes | |
| A stack that wants a cluster belongs on the gateway | | placement takes a `cluster` need the test does not yet pass |
| Nothing about an installed app is stored on the hub | live read only | |
| Publish a stack's port | in `sharing_test.sh` | |
| Pull runs and restarts the stack — no lost `-f` leaving it stopped | `test_pull_action_keeps_the_stack_running` | |
| A deploy that cannot bind its port leaves no stack behind (or is marked failed) | `test_deploy_binding_a_taken_port_fails_and_cleans_up` | |
| A compose file with a refusal-list entry (`privileged`, `pid: host`, …) deploys with a warning for an admin | `test_privileged_compose_deploys_with_a_warning_for_an_admin` | the agent-token refusal path (vs. admin warning) needs a job token calling `POST /hosts/{h}/apps` |
| Deleting a catalog entry that was never added is refused | `test_deleting_a_catalog_entry_that_was_never_added_is_refused` | |

## docs/pooling/tasks.md — `tasks_test.sh`

| Claim | Runs | Untested because |
| --- | --- | --- |
| A task is a command, a host and a schedule; the tab lists them; no crontab anywhere | yes | |
| Run now fires it; every fire is recorded with exit code and output | yes | |
| A fan-out names every remote, one machine at a time | fan-out yes | "one at a time" is not asserted |
| The per-task timeout ends a hanging command | yes | |
| A task that starts failing, and one that recovers, is one transition each | yes | |
| An offline host is skipped and recorded; runs never stack; a hub down over a tick makes nothing up | | needs an offline remote at fire time (see `storage_test.sh` for the only place a VM is stopped) and a real schedule tick |
| The gate is applied on save and on every fire | on fire | |
| Run now on a disabled task is refused | `test_run_now_on_a_disabled_task_is_refused` | |
| A timeout of 0 is refused; the cap is 86400s | `test_task_timeout_is_bounded` | |

## docs/pooling/storage.md — `storage_test.sh`, `workspaces_test.sh`

| Claim | Runs | Untested because |
| --- | --- | --- |
| A cluster's capacity is its members' added; two or more members on a gateway | yes | |
| A member carrying the system disk is refused | yes | swap and the cluster's own mount are not tried |
| Removing a member rebuilds the mount without it; re-adding works | yes | |
| A member that is off is a hole, not a hang; the cluster reads Degraded and names it; healthy again when it returns | yes (stops remote-mid's VM; slow) | |
| A new file lands on the member with most free space; a file is never split | | needs disks small enough to fill in a test |
| Creating a cluster from spare disks | | the lab's disks are all in `pool` |
| A workspace appears at one path on every named remote; a file one writes the other reads | yes | |
| Removing a remote closes its share and the files stay; deleting the workspace leaves the files | yes | |
| Jobs on separate remotes run at once on one workspace | | needs two jobs whose effect on shared files is deterministic |
| A workspace never crosses a space | | no API path could make it; nothing to assert |
| A raw disk with no filesystem is listed as unformatted, not silently dropped | `test_raw_disks_are_listed_as_unformatted` (`hosts_test.sh`, over `GET /api/hosts`'s `facts.disks`) | |

## docs/pooling/inference.md — `inference_test.sh`

| Claim | Runs | Untested because |
| --- | --- | --- |
| One endpoint in Ollama's shape; a model name matches exactly | yes | |
| A machine can be taken out of the pool without removing anything | yes | |
| The decision is visible: host and reason on every response | yes | |
| One decision per conversation, keyed on history | yes | "when that place is gone, the next turn is a first turn" is not provoked |
| Streaming requests stream | yes | |
| The Models grid: every machine, every model, free space | yes | |
| Pull a model to a machine; remove it | yes (one small model) | pulling to several at once |
| What fits asks llmfit on the machine | yes | the card suggesting a model from the same answer |
| Install / remove Ollama on a card | | remote-big is the lab's only Ollama; removing it breaks every other suite |
| Busy machines queue to a depth you set | | one CPU machine cannot be made busy deterministically |
| A pull that fails (bogus model name) says why, instead of the row silently disappearing | `test_pulling_a_bogus_model_says_why` | |

## docs/pooling/agents/ — `agents_test.sh`

| Claim | Runs | Untested because |
| --- | --- | --- |
| A job runs on the remote as its own agent and ends with a report; events are copied live; history per host | yes | |
| The per-job time you set is recorded on the job | yes | a job actually running out of it needs an agent that obeys "sleep" |
| Rounds are capped; at the cap the job is needs you, and that is an event | yes | |
| A correction is a follow-up round on the same session | yes | |
| Roll back: btrfs live, LVM at boot, ext4 says none and points at the rebuild script | ext4 branch | the lab's roots are ext4; the other two branches are asserted if a lab has them |
| A Pi-class box refuses a job with the reason | yes | |
| Re-provision runs the layout again without a new code or host record | yes | |
| Fleet default model in Settings; card override; the card shows what the remote reported | override via `/agent` | |
| The Agents tab's windows: open, reattach, no local tools | open + attach renders (`tests/ui/agents.spec.js`) | reattach and "no local tools" are not driven |
| Job history is trimmed on a retention; the last few per host stay | | needs enough history and a clock |
| A job with a `cwd` the remote reports missing is refused | `test_a_job_with_a_missing_cwd_is_refused` | |
| A session is named by the hub (adjective-noun), a sent name is ignored, and no two live sessions share one | `test_session_names_are_drawn_by_the_hub_and_unique_while_live` | |
| A closed session keeps its last screen: 409 while live, omp's terminal stream once closed | `test_a_closed_session_keeps_its_last_screen` | the History toggle in the panel is not driven |
| A host card's model override is the same provider/model picker as Settings | | not driven; `ui/panel.spec.js` covers the Settings picker only |
| A Pi-class box's re-provision skips the credential snapshot step (`memTotal < 1.4 GB`) instead of failing the whole run | `test_reprovision_on_a_pi_class_box_skips_the_snapshot_not_the_run` | |
| A window renders `omp` with no uncaught error (xterm's es2022 build) | `ui/agents.spec.js` | |
| A model is `provider/model`; the provider's own id may itself contain slashes (`openrouter/openai/gpt-4o-mini`) | `test_agent_default_model_allows_a_second_slash` (`accounts_test.sh`); manually verified end to end (Task A, 2026-09-16): a window and a remote job both ran through OpenRouter with the vaulted key | |
| A model is picked as a provider from omp's list plus the provider's own id, saved as `provider/id`; `GET /api/agents/models` lists what the vault can run | `ui/panel.spec.js` "the model pickers save provider and id" | |
| The hub and its remotes can run different models: `agent.default_model` is the hub's, `agent.remote_model` the remotes' (blank: the hub's) | `test_agent_remote_model_is_its_own_setting` (`accounts_test.sh`) | a job actually running the remote model needs a vaulted key |
| Secrets: names listable, values never; `homedash-secret NAME` answers a granted host, refuses another; every read and refusal is an event; a deleted secret is gone | yes | |
| Credentials: Update mints and pulls; Revoke leaves a token the hub no longer answers | both calls succeed and revoke is an event | that the revoked token really opens nothing needs the vault protocol driven from the remote |
| The rebuild script: appended on the hub's own initiative (model, lock); readable and editable | yes | folding a job's report into it needs a job that changes the machine deterministically |

## docs/pooling/notifications.md — `notifications_test.sh`

| Claim | Runs | Untested because |
| --- | --- | --- |
| The target is a setting; a transition is POSTed to it; sending never blocks | yes (the POST assertion skips if the one-shot listener loses the race) | |
| The fullness threshold is a setting | yes | crossing it needs a disk that fills |
| Each named transition kind reaches the target | host lock; task failing/ok, job needs you, secret read, gate refusal, restore and reprovision are asserted as events in their own suites | offline/back, key mismatch, cluster Degraded (asserted as state, not event), disk full, credential pull failed, peer refused (asserted in `sharing_test.sh`) |
| Events page backward on `?before=<id>&limit=<n>`, capped at 500 | `test_events_page_backward_by_id` | the panel's "Older" button and `homedash events --json --before` are not yet driven from a test |

## docs/sharing/ — `sharing_test.sh`

| Claim | Runs | Untested because |
| --- | --- | --- |
| Hubs find each other over the DHT; a peer is its connection's key | yes | |
| The offer lists the peer's models | yes | free-machine and stale-offer handling |
| Inference falls back to a peer when the local pool cannot serve; the decision names the peer | yes | |
| Unapproved: the peer refuses before reading, and records it | yes | |
| Trust is one switch both ways: A never sends to a peer it has not approved | yes | |
| Per-hour allowance meters a key | yes | max concurrent, the hub-wide ceilings, unknown-peer default, bandwidth: need parallel load or a third house |
| The score counts sent and served by model, both directions | yes | bytes fronted/origin |
| The record (accept rate, finish rate, first-token time) chooses among peers | unit | one peer only; nothing to choose between |
| A gateway with no machines goes straight to its space | | hub-b has remote-b; emptying it breaks the other direction |
| Publish a service to named peers; the front approves; served as a link, proxied from the origin | yes | |
| A service published to nobody is invisible; unpublishing drops it from the next offer | yes | |
| A service names only a machine this hub owns | yes | |
| Ingress: a hostname the fronting side picks; a non-DNS name is refused | up to the name | no public DNS or certificate in the lab |
| One hop: a job from a peer is never forwarded | structural (`hub_curl_local` uses the inbound header) | |
| Only chat/generate/embed cross; pull/push/create are refused | unit | |
| Private DHT, bootstrap hubs, network key | unit (`TestNetConfig`) | the lab's houses reach the public DHT; a private one needs a reachable hub |
| Only a prompt path (chat/generate/embed) is ever tried on a peer; a model lookup (`/api/show`) is answered at home, no `peer.refused` recorded | `test_show_for_an_unknown_model_stays_home` | |
| A boolean setting (`space.serve`, …) is canonicalised to `true`/blank on write, whatever vocabulary it was sent in | `test_settings_booleans_write_the_canonical_value` | |
| A service publish refuses port 22 and the hub's own port by default | `test_publish_refuses_the_hubs_and_sshs_ports` | |
| Publishing an existing service name is refused unless `replace: true` | `test_publishing_a_name_twice_needs_replace` | |

## docs/running/README.md — `backup_test.sh`

| Claim | Runs | Untested because |
| --- | --- | --- |
| An export needs a passphrase | yes | |
| Export downloads one age-encrypted file; status records when | yes | |
| Restore with the wrong passphrase is refused with nothing staged | yes | |
| Restore puts the export back; the hub restarts itself; recorded as an event | yes (hub-a, its own export) | |
| A fresh hub's setup page takes an export before any passkey | | needs a hub with no account; hub-b has one after `ui/` runs |
| `homedash export` / `homedash restore` on the hub's shell | | the restore needs the hub stopped |
| One binary installs as hub, panel and CLI; uninstall keeps the state directory | `lab/make deploy` installs the .deb | uninstall is not run |

## docs/running/README.md — `health_test.sh`, `ui/panel.spec.js`

| Claim | Runs | Untested because |
| --- | --- | --- |
| The hub reports its own machine (cores, memory, load, uptime) and the disk its state file sits on | `test_the_hub_reports_its_own_machine_and_state_file` | |
| One line per thing the hub keeps running, each ok/warn/bad/off with a reason; the whole is the worst line | `test_every_line_has_a_state_and_a_reason` | |
| The Hosts line counts what the Hosts tab shows | `test_the_hosts_line_counts_what_the_hosts_tab_shows` | |
| Health is behind sign-in like every other `/api` path | `test_health_is_behind_sign_in` | |
| The Health tab draws the report: hostname, every line with its state, the rail LED the worst of them | `ui/panel.spec.js` "the Health tab draws exactly what GET /api/hub/health reports" | |
| The disk line turns red at `notify.disk_percent`, the same threshold a remote's mount uses | | filling the hub's disk is not something the suite does to a lab hub |
| Nothing on the page is a control | structural: `Health.svelte` has no button | |

## The API — `cli_test.sh`, `ui/panel.spec.js`

Claims from docs/running/README.md's "The API" paragraph and the doc line
PR 2 added: an unknown `/api` path answers 404, never the panel's HTML.

| Claim | Runs | Untested because |
| --- | --- | --- |
| An unknown `/api` path is a 404, plain text, not the panel's `index.html` | `test_an_unknown_api_path_is_404_not_the_panel` (`cli_test.sh`) | |
| A 502 (or any non-2xx) with an HTML body reads as one sentence in the panel, never the raw markup | `ui/panel.spec.js` "a 502 with an HTML body reads as one sentence, not raw markup" | |
| A failed list read shows the error, not the empty-state copy beside it | `ui/panel.spec.js` "a 502 on /api/hosts shows the error, not the empty state" | |
| A 401 (session gone server-side) returns the panel to the sign-in card instead of leaving a dead shell up | `ui/panel.spec.js` "a session that no longer exists returns to the sign-in card" | |
| Opening every tab throws no uncaught error | `ui/panel.spec.js` "every tab opens once signed in" (now asserts zero `pageerror`s) | |

## hub/ui/README.md — `ui/panel.spec.js`

| Claim | Runs | Untested because |
| --- | --- | --- |
| Every tab fits a phone (390px) with no page-level horizontal scroll | `ui/panel.spec.js` "at 390px no tab widens the page itself" | 860px and 1280px overflow (X-9) are fixed by the same `.scroll` wrapper but not separately asserted |
| The tab strip stays pinned under 860px and scrolls the active tab into view | `ui/panel.spec.js` "at 390px no tab widens the page itself" | |
| On a coarse pointer every control is ≥38px tall and text inputs are 16px | `ui/panel.spec.js` "controls are tall enough to hit and fields do not zoom" | only the Tasks form; the rule is one `(pointer: coarse)` block in `app.css` |
| Under 560px a `table.stack` lays each row out as a card with its detail row full width | — | untested: a layout claim with no functional signal; seen in a 390px screenshot pass |
| The panel installs to a home screen (`manifest.webmanifest`, icons) | — | untested: a browser-side install prompt with no lab signal |
| Escape dismisses an open inline card | `ui/panel.spec.js` "Escape closes the New remote card" | only Hosts' New remote card; the other inline forms share the same mechanism |
| A tab's hash is matched loosely, not only its exact lowercase id | `ui/panel.spec.js` "#Hosts, capitalised, still opens Hosts" | |

## docs/running/accounts.md — `accounts_test.sh`, `ui/accounts.spec.js`, `ui/panel.spec.js`

| Claim | Runs | Untested because |
| --- | --- | --- |
| Passkey register with a code, sign out, sign in; wrong and used codes refused | yes (browser) | |
| An admin invites with a single-use code; a viewer cannot | yes | |
| An API token is shown once; a deleted one opens nothing | yes | |
| Two roles; a viewer reads, cannot write, may use the router; enforced at the API | yes | |
| The panel keeps asking for a second passkey until there are two | | `ui/` registers one passkey per account |
| Locked out, a shell on the hub prints a one-time code | `homedash invite` is how `ui/` gets its codes | |
| A token name is unique; a second mint of the same name is refused (409), not a crash in Settings | `test_a_duplicate_token_name_is_refused` (`accounts_test.sh`), `ui/accounts.spec.js` "a duplicate token name is refused with a visible message, not a crash" | |
| An invite can be revoked before it is used | `test_a_revoked_invite_cannot_be_used` | |
| Demoting an admin to viewer is refused only when it is the last one | `test_the_last_admin_cannot_be_demoted` | self-demotion with another admin already present (the ordinary case) is exercised by the probe, not this suite |
| The panel greys what a viewer cannot do; the API still refuses — a viewer's action error (403) stays visible past the next poll, not wiped by `load()` | `ui/panel.spec.js` "a viewer sees the panel without the keyboard" (Hosts Revoke, Tasks Delete, Settings Make-a-token) | Agents/Peers/Apps/Network/Models viewer paths are exercised by the probe, not this suite |
| A viewer's window says read-only in the status line | | `ui/agents.spec.js` opens a window as an admin only; a viewer-attach case is not yet driven |

## docs/running/cli.md — `cli_test.sh`, `ui/cli.spec.js`

| Claim | Runs | Untested because |
| --- | --- | --- |
| `login` through the browser; the session works; `logout` ends it; a grant is spent once | yes (browser) | |
| `whoami`; not signed in says so | yes | |
| Every subcommand takes `--json` and prints exactly what the API returned | hosts, devices, storage, apps, catalog, place, secrets, events, jobs | logs, fit, job, lock, name, deploy, app, start, correct, rollback, reprovision — the API behind each is covered in its own suite |
| `run` executes one gated command; `write` puts a file; `rebuild` reads the script | yes | `write --sudo`, `rebuild -f` |
| A viewer's CLI reads; an admin's does everything | yes | |
| Minting a token under a name that already exists replaces it (one row, a fresh value), rather than erroring or duplicating | `test_token_name_replaces_the_previous_one` | |

## docs/running/safety.md — `safety_test.sh`

| Claim | Runs | Untested because |
| --- | --- | --- |
| The gate refuses: firewall off, SSH cut, SSH config edited, disk wiped, system path deleted, reboot; also after a `;` | yes | |
| An ordinary command runs; the list is published at the API; every refusal is an event | yes | |
| The hub is not a host the gate can name; nothing reaches SSH without a token | yes | |
| A file write goes through the same gate | yes | |
| The agent account has no sudo, cannot touch the hub's hold, is caged from the house | yes | |
| The agent's unit: system read-only, its home writable | yes (the unit shape the hub uses, run the same way) | task cap, private /tmp, wall clock |
| `homedash-sudo` runs an allowed command as root, refuses protected paths, shells, privileged containers; every use is an event; `jobs.sudo` off closes the door | yes | |
| Host key pinning refuses a changed key | | see hosts.md |
| Roll back undoes a job | see agents | |
