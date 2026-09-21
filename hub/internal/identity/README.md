# internal/identity

The hub's SSH key: `hub_key` (Ed25519, OpenSSH PEM, mode 0600) and
`hub_key.pub` (one `authorized_keys` line) in the state directory,
generated on first start if absent. The public line is what enrollment
copies to a remote and what Settings shows; the private half is loaded
into the SSH executor's signer and never leaves the hub. There is no
rotation: a new key would mean re-enrolling every remote.
