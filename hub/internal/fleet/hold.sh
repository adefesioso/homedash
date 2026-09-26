# The hub's hold, checked and put back as root, twice per job round:
# first as the round's own unit's ExecStopPost (which runs even when the
# job timed out, was killed, or broke the hub's sudo), writing what it did
# to $LAST; then by the hub over the round's connection, which prints
# $LAST and its own result. PUBKEY is set above this text. Prints what it
# restored, one word each, or nothing; a line starting "broken:" is
# something it could not put back and a person must look at.
ACCOUNT=homedash
LAST=/etc/homedash/hold-restored
[ -t 1 ] || [ "$(readlink -f /proc/$$/fd/1)" = "$LAST" ] || { cat "$LAST" 2>/dev/null; rm -f "$LAST"; }
fixed=""
if ! getent passwd "$ACCOUNT" >/dev/null; then
  useradd --create-home --shell /bin/bash "$ACCOUNT" >/dev/null 2>&1
  usermod -aG docker "$ACCOUNT" >/dev/null 2>&1 || true
  fixed="$fixed account"
fi
key=/etc/ssh/authorized_keys.d/$ACCOUNT
if [ "$(cat "$key" 2>/dev/null)" != "$PUBKEY" ]; then
  install -d -m 0755 /etc/ssh/authorized_keys.d
  chattr -i "$key" 2>/dev/null || true
  printf '%s\n' "$PUBKEY" > "$key"; chmod 0644 "$key"
  chattr +i "$key" 2>/dev/null || true
  fixed="$fixed key"
fi
dropin=/etc/ssh/sshd_config.d/10-homedash-keys.conf
want='# Written by HomeDash. The hub'"'"'s key is root-owned under /etc/ssh, not under a home.
AuthorizedKeysFile /etc/ssh/authorized_keys.d/%u .ssh/authorized_keys'
if [ "$(cat "$dropin" 2>/dev/null)" != "$want" ]; then
  install -d -m 0755 /etc/ssh/sshd_config.d
  printf '%s\n' "$want" > "$dropin"
  fixed="$fixed dropin"
fi
sudoers=/etc/sudoers.d/$ACCOUNT
if [ "$(cat "$sudoers" 2>/dev/null)" != "$ACCOUNT ALL=(ALL) NOPASSWD:ALL" ]; then
  chattr -i "$sudoers" 2>/dev/null || true
  printf '%s ALL=(ALL) NOPASSWD:ALL\n' "$ACCOUNT" > "$sudoers"; chmod 0440 "$sudoers"
  chattr +i "$sudoers" 2>/dev/null || true
  fixed="$fixed sudoers"
fi
systemctl is-enabled ssh >/dev/null 2>&1 || systemctl is-enabled sshd >/dev/null 2>&1 || { systemctl enable ssh >/dev/null 2>&1 || true; fixed="$fixed ssh-enabled"; }
if sshd -t 2>/dev/null; then
  if [ -n "$fixed" ]; then systemctl reload ssh >/dev/null 2>&1 || systemctl reload sshd >/dev/null 2>&1 || true; fi
  systemctl is-active ssh >/dev/null 2>&1 || systemctl is-active sshd >/dev/null 2>&1 || { systemctl start ssh >/dev/null 2>&1 || true; fixed="$fixed ssh-started"; }
else
  echo "broken: sshd rejects its own config"
fi
au=$(sshd -T 2>/dev/null | awk '$1=="allowusers"{$1="";print}')
[ -z "$au" ] || printf '%s\n' "$au" | grep -qw "$ACCOUNT" || echo "broken: sshd AllowUsers leaves out $ACCOUNT"
printf '%s\n' "${fixed# }"
