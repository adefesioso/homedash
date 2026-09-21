# The lock, the address, the model, what fits

`SetLock` writes an sshd drop-in (`PasswordAuthentication no`,
`PermitRootLogin no`, `AllowUsers homedash`) as a candidate, has `sshd -T`
say what it would allow for the hub's account and for `nobody`, and only
then moves it into place and reloads; either check failing removes it.

`SetAddress` holds an interface at a static address, or hands it back
to DHCP. It pipes `address.sh` as root: the script picks the manager
that owns the machine's network (NetworkManager if it is up and has the
interface; else netplan if installed; else systemd-networkd if up; else
ifupdown), writes the config under HomeDash's name for that manager —
`90-homedash-IFACE.yaml`, `00-homedash-IFACE.network`,
`interfaces.d/homedash-IFACE` with the interface's other stanzas
commented out and conflicting drop-ins moved aside, or `nmcli con mod`
on the interface's connection — disables cloud-init's network rendering
where cloud-init exists, and starts a **guard** as a transient systemd
unit that applies the config and then waits ninety seconds for
`/run/homedash-net-confirm`, reverting to the saved config if it never
appears. The hub dials the new address with the pinned host key until
it answers, touches the confirm file, re-pins `hosts.addr`, drops the
held connection, and sweeps; a mismatch or a timeout is an error and
the machine reverts on its own. A second change while the guard is
still up is refused by the script.

`SetAgentModel` writes the card's override into the remote's
`config.yml`; blank means the fleet's remote model (`RemoteModel`:
`agent.remote_model`, or `agent.default_model` when that is blank),
which is also what enrollment's layout writes and a job runs. `Fit` runs
`llmfit recommend --json -n 400` and keeps the entries with an Ollama
name, best first; nothing is stored.
