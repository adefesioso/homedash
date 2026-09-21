# The forwards and the job door

Every `omp` the hub runs over SSH — a job round, a credential pull, a
command run "with forwards" — gets two reverse forwards on the remote's
loopback for the life of that connection: `8765` to a **vault proxy** for
that host and `11435` to that connection's **job door**. The proxy is a
loopback listener on the hub that accepts only this host's own bearer
token (`hosts.vault_token`, minted at every credential delivery, emptied
by **Revoke**) and swaps in the real one before passing the request to
the vault. The door is a loopback `http.Server` on the hub serving exactly
three things: the router (Ollama's paths, so `homedash/<model>` works),
`GET /secrets/{name}` for that host's grants, and `POST /sudo` — the
command in the body, checked by `gate.Check` and `gate.Privileged`, run
as root through `sudo -n sh -c` on the same connection, its merged
output returned with the exit code in a header, and every request
recorded as a `sudo.run` or `sudo.refused` event and as a job event when
the connection is a job's. `jobs.sudo` off answers every `/sudo` with
403. The panel's listener is never forwarded, so a remote's agent has
no route to the hub's API. `homedash-secret NAME` and `homedash-sudo …`
are curls of the door. Before each omp run the provider cache is removed
and `omp models` rebuilds it through the door, because omp resolves
`--model` from that cache and never refreshes it when the pool gains a
model.

## Held connections

`Hold` keeps one SSH client per host for traffic that would otherwise
dial on every use — a published service's connections — redialing when
the kept one has died and dropping it when the host is removed. `Port`
opens a `direct-tcpip` channel on that client to a port on the remote's
loopback: this is a service's origin side.
