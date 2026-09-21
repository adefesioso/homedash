# The layout: everything HomeDash leaves on a remote. Runs as root, is
# idempotent, and is the body of enrollment as well as of Re-provision.
ACCOUNT=homedash
AGENT=homedash-agent

say() { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
[ "$(id -u)" = 0 ] || { echo "the layout runs as root" >&2; exit 1; }
command -v apt-get >/dev/null || { echo "HomeDash remotes are Debian-family machines (apt-get not found)" >&2; exit 1; }
export DEBIAN_FRONTEND=noninteractive

say "packages"
apt-get -qq update >/dev/null
apt-get -qq install -y curl ca-certificates openssh-server nftables rsync >/dev/null
[ "$(findmnt -no FSTYPE / 2>/dev/null)" = btrfs ] && apt-get -qq install -y btrfs-progs >/dev/null
systemctl enable --now ssh >/dev/null 2>&1 || true

say "the hub's account"
if ! getent passwd "$ACCOUNT" >/dev/null; then
  useradd --create-home --shell /bin/bash "$ACCOUNT"
fi
HOME_DIR=$(getent passwd "$ACCOUNT" | cut -d: -f6)
# The hub's key lives root-owned under /etc/ssh, named by an sshd drop-in,
# so the hub's access depends on nothing under a writable home.
install -d -m 0755 /etc/ssh/authorized_keys.d
chattr -i /etc/ssh/authorized_keys.d/"$ACCOUNT" 2>/dev/null || true
printf '%s\n' "$PUBKEY" > /etc/ssh/authorized_keys.d/"$ACCOUNT"
chmod 0644 /etc/ssh/authorized_keys.d/"$ACCOUNT"
install -d -m 0755 /etc/ssh/sshd_config.d
printf '# Written by HomeDash. The hub'"'"'s key is root-owned under /etc/ssh, not under a home.\nAuthorizedKeysFile /etc/ssh/authorized_keys.d/%%u .ssh/authorized_keys\n' > /etc/ssh/sshd_config.d/10-homedash-keys.conf
if sshd -t 2>/dev/null; then
  systemctl reload ssh >/dev/null 2>&1 || systemctl reload sshd >/dev/null 2>&1 || true
  # The copy under the home, if an earlier layout left one, goes.
  if [ -f "$HOME_DIR/.ssh/authorized_keys" ]; then
    grep -vF "$PUBKEY" "$HOME_DIR/.ssh/authorized_keys" > "$HOME_DIR/.ssh/authorized_keys.new" || true
    mv -f "$HOME_DIR/.ssh/authorized_keys.new" "$HOME_DIR/.ssh/authorized_keys"
  fi
else
  echo "sshd rejected the drop-in; keeping the key under the home as well" >&2
  install -d -m 0700 -o "$ACCOUNT" -g "$ACCOUNT" "$HOME_DIR/.ssh"
  touch "$HOME_DIR/.ssh/authorized_keys"
  grep -qF "$PUBKEY" "$HOME_DIR/.ssh/authorized_keys" || printf '%s\n' "$PUBKEY" >> "$HOME_DIR/.ssh/authorized_keys"
  chown "$ACCOUNT:$ACCOUNT" "$HOME_DIR/.ssh/authorized_keys"; chmod 0600 "$HOME_DIR/.ssh/authorized_keys"
fi
chattr +i /etc/ssh/authorized_keys.d/"$ACCOUNT" 2>/dev/null || true
# The hub installs stacks, sets locks and mounts disks through this account.
chattr -i /etc/sudoers.d/homedash 2>/dev/null || true
printf '%s ALL=(ALL) NOPASSWD:ALL\n' "$ACCOUNT" > /etc/sudoers.d/homedash; chmod 0440 /etc/sudoers.d/homedash
chattr +i /etc/sudoers.d/homedash 2>/dev/null || true

say "docker"
if ! command -v docker >/dev/null 2>&1; then
  curl -fsSL https://get.docker.com | sh >/dev/null
fi
usermod -aG docker "$ACCOUNT" || true

say "the agent's account"
# A job runs here: no sudo, ever; the hub account's group so shared
# storage is writable; and the docker group, so a job runs containers on
# its own box — root by another name, held by the hook, not the kernel.
if ! getent passwd "$AGENT" >/dev/null; then
  useradd --create-home --shell /bin/bash -G "$ACCOUNT",docker "$AGENT"
fi
usermod -aG "$ACCOUNT",docker "$AGENT"
for g in sudo adm; do gpasswd -d "$AGENT" "$g" >/dev/null 2>&1 || true; done
rm -f /etc/sudoers.d/"$AGENT"
AGENT_HOME=$(getent passwd "$AGENT" | cut -d: -f6)
chmod 0750 "$AGENT_HOME"

say "the agent (omp $OMP_VERSION)"
case "$(uname -m)" in
  x86_64|amd64) ASSET=omp-linux-x64 ;;
  aarch64|arm64) ASSET=omp-linux-arm64 ;;
  *) echo "no omp build for $(uname -m)" >&2; exit 1 ;;
esac
BIN=/usr/local/bin
if ! [ -x "$BIN/omp" ] || [ "$("$BIN/omp" --version 2>/dev/null)" != "omp/${OMP_VERSION#v}" ]; then
  curl -fsSL "$OMP_RELEASE$ASSET" -o "$BIN/omp.part"
  want=$(curl -fsSL "${OMP_RELEASE}SHA256SUMS.txt" | awk -v a="$ASSET" '$2==a||$2=="*"a{print $1}')
  got=$(sha256sum "$BIN/omp.part" | cut -d' ' -f1)
  [ "$want" = "$got" ] || { echo "omp checksum mismatch" >&2; rm -f "$BIN/omp.part"; exit 1; }
  chmod 0755 "$BIN/omp.part"; mv "$BIN/omp.part" "$BIN/omp"
fi
chown root:root "$BIN/omp"; chmod 0755 "$BIN/omp"
install -d -m 0700 -o "$AGENT" -g "$AGENT" "$AGENT_HOME/.omp" "$AGENT_HOME/.omp/agent" "$AGENT_HOME/.omp/cache"
if [ -n "$DEFAULT_MODEL" ] && ! [ -f "$AGENT_HOME/.omp/agent/config.yml" ]; then
  printf 'modelRoles:\n  default: %s\n' "$DEFAULT_MODEL" > "$AGENT_HOME/.omp/agent/config.yml"
  chown "$AGENT:$AGENT" "$AGENT_HOME/.omp/agent/config.yml"
fi
cat > "$AGENT_HOME/.omp/agent/models.yml" <<'MODELS'
{{.ModelsYML}}
MODELS
chown "$AGENT:$AGENT" "$AGENT_HOME/.omp/agent/models.yml"; chmod 0600 "$AGENT_HOME/.omp/agent/models.yml"
# The hook is root-owned in a root-owned directory: the agent traverses it and cannot change it.
install -d -m 0755 -o root -g root "$AGENT_HOME/.omp/agent/hooks" "$AGENT_HOME/.omp/agent/hooks/pre"
cat > "$AGENT_HOME/.omp/agent/hooks/pre/homedash-refusals.ts" <<'HOOK'
{{.Hook}}
HOOK
chmod 0644 "$AGENT_HOME/.omp/agent/hooks/pre/homedash-refusals.ts"
cat > "$BIN/homedash-secret" <<'HELPER'
{{.SecretHelper}}
HELPER
cat > "$BIN/homedash-sudo" <<'HELPER'
{{.SudoHelper}}
HELPER
chmod 0755 "$BIN/homedash-secret" "$BIN/homedash-sudo"
grep -q OMP_AUTH_BROKER_URL "$AGENT_HOME/.profile" 2>/dev/null || printf '\nexport OMP_AUTH_BROKER_URL=http://127.0.0.1:8765\n' >> "$AGENT_HOME/.profile"
chown "$AGENT:$AGENT" "$AGENT_HOME/.profile"
# An earlier layout kept the agent under the hub's account; that goes.
rm -rf "$HOME_DIR/.omp" "$HOME_DIR/.local/bin/omp" "$HOME_DIR/.local/bin/homedash-secret" "$HOME_DIR/.local/bin/llmfit"

say "the cage"
install -d -m 0755 /etc/homedash
AGENT_UID=$(id -u "$AGENT")
cat > /etc/homedash/agent-cage.nft <<CAGE
#!/usr/sbin/nft -f
# Written by HomeDash. The cage: the agent account reaches loopback and the
# internet; the house — the hub, the other remotes, every private address — it does not.
table inet homedash-agent
delete table inet homedash-agent
table inet homedash-agent {
  set private4 { type ipv4_addr; flags interval; elements = { 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 169.254.0.0/16, 100.64.0.0/10 } }
  chain output {
    type filter hook output priority filter; policy accept;
    meta skuid $AGENT_UID oifname "lo" accept
    meta skuid $AGENT_UID ip daddr @private4 counter reject with icmp type admin-prohibited
    meta skuid $AGENT_UID ip6 daddr { fc00::/7, fe80::/10 } counter reject
  }
}
CAGE
cat > /etc/systemd/system/homedash-agent-cage.service <<'UNIT'
[Unit]
Description=HomeDash: the agent account's network cage
DefaultDependencies=no
Before=network-pre.target
Wants=network-pre.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/sbin/nft -f /etc/homedash/agent-cage.nft
ExecStop=/usr/sbin/nft delete table inet homedash-agent

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable homedash-agent-cage.service >/dev/null 2>&1
systemctl restart homedash-agent-cage.service

say "llmfit $LLMFIT_VERSION"
case "$(uname -m)" in x86_64|amd64) FIT=x86_64 ;; *) FIT=aarch64 ;; esac
FIT_TGZ="llmfit-$LLMFIT_VERSION-$FIT-unknown-linux-gnu.tar.gz"
if ! [ -x "$BIN/llmfit" ] || [ "$("$BIN/llmfit" --version 2>/dev/null)" != "llmfit ${LLMFIT_VERSION#v}" ]; then
  tmp=$(mktemp -d)
  curl -fsSL "$LLMFIT_RELEASE$FIT_TGZ" -o "$tmp/fit.tgz"
  want=$(curl -fsSL "$LLMFIT_RELEASE$FIT_TGZ.sha256" | awk '{print $1}')
  got=$(sha256sum "$tmp/fit.tgz" | cut -d' ' -f1)
  [ "$want" = "$got" ] || { echo "llmfit checksum mismatch" >&2; exit 1; }
  tar -xzf "$tmp/fit.tgz" -C "$tmp"
  install -m 0755 "$(find "$tmp" -type f -name llmfit | head -1)" "$BIN/llmfit"
  rm -rf "$tmp"
fi
