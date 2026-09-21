# packaging

`nfpm.yaml` builds the `.deb` for `amd64` and `arm64` from
`dist/homedash-linux-<arch>`: the binary to `/usr/bin/homedash`, the
unit to `/lib/systemd/system/homedash.service`, the desktop entry and
icon under `/usr/share`. No Debian toolchain is needed.

- `homedash.service` runs `homedash serve` as the `homedash` system
  user with `StateDirectory=homedash`, restarting on failure, hardened
  (`ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`,
  `NoNewPrivileges`) and granted `CAP_NET_BIND_SERVICE` so an ingress
  front can bind `:80` and `:443` without running as root.
- `homedash.desktop` opens the panel through the launcher.
- `apt-repo.sh` is not built into the package: it is run once on an
  installed machine to feed a local apt repository from the GitHub
  releases, so upgrades arrive with `apt upgrade` — see
  [keeping it updated](../../docs/running/updating.md).
- `postinst.sh` creates the account and the state directory and
  enables the service; `prerm.sh` stops the service and removes the
  hub's copy of `omp` (`/var/lib/homedash/omp/bin`); `postrm.sh` on
  purge removes the account and never the rest of `/var/lib/homedash`,
  because that is the part you can't rebuild.
