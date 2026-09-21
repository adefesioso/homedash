#!/bin/sh
# Put an apt index in front of the GitHub releases page, on this machine,
# so HomeDash upgrades with `apt upgrade` like any other package.
# See docs/running/updating.md.
set -eu

REPO="${HOMEDASH_REPO:-adefesioso/homedash}"
DIR="${HOMEDASH_APT_DIR:-/var/lib/homedash-apt}"
SYNC=/usr/local/bin/homedash-apt-sync
SOURCE=/etc/apt/sources.list.d/homedash.sources
HOOK=/etc/apt/apt.conf.d/99homedash-apt-sync

die() { echo "apt-repo: $*" >&2; exit 1; }

usage() {
  cat <<USAGE
usage: ${0##*/} [--uninstall]

Installs a local apt repository fed from ${REPO}'s releases, plus an apt
hook that refreshes it on every \`apt update\`. Run once, as root, on a
machine that has homedash installed.

  --uninstall   remove the repository, the sync script and the hook
                (the installed homedash package is left alone)

environment:
  HOMEDASH_REPO      GitHub owner/name  (default ${REPO})
  HOMEDASH_APT_DIR   repository path    (default ${DIR})
  GITHUB_TOKEN       optional, only raises the anonymous rate limit
USAGE
}

case "${1:-}" in
  -h|--help) usage; exit 0 ;;
  --uninstall)
    [ "$(id -u)" = 0 ] || die "must run as root"
    rm -f "$SOURCE" "$HOOK" "$SYNC"
    rm -rf "$DIR"
    apt-get update >/dev/null 2>&1 || true
    echo "apt-repo: removed. The homedash package itself is untouched."
    exit 0
    ;;
  "") ;;
  *) usage >&2; exit 2 ;;
esac

[ "$(id -u)" = 0 ] || die "must run as root"
command -v curl >/dev/null 2>&1 || die "curl is required (apt install curl)"
command -v dpkg-scanpackages >/dev/null 2>&1 ||
  die "dpkg-scanpackages is required (apt install dpkg-dev)"

install -d -m 0755 "$DIR"

# --- the sync script -------------------------------------------------
# Two lines of configuration, expanded now so the installed script needs
# no arguments and no environment later; then the body, verbatim.
cat > "$SYNC" <<CONFIG
#!/bin/sh
# Fetch the newest homedash .deb for this machine and rebuild the local
# apt index. Installed by hub/packaging/apt-repo.sh — edit that, not this.
# Never fails hard: an unreachable GitHub leaves the previous index in place.
set -eu
REPO='${REPO}'
DIR='${DIR}'
CONFIG

cat >> "$SYNC" <<'BODY'
API="https://api.github.com/repos/$REPO/releases/latest"

warn() { echo "homedash-apt-sync: $*" >&2; }

arch=$(dpkg --print-architecture)

set -- -fsSL --retry 2 --max-time 60
if [ -n "${GITHUB_TOKEN:-}" ]; then
  set -- "$@" -H "Authorization: Bearer $GITHUB_TOKEN"
fi

if ! json=$(curl "$@" "$API" 2>/dev/null); then
  warn "could not reach GitHub; keeping the current index"
  exit 0
fi

# No jq on a stock Debian box, so pull the asset URL out with grep.
url=$(printf '%s\n' "$json" |
  grep -o "https://[^\"]*/homedash_[^\"]*_${arch}\.deb" |
  head -n 1) || true

if [ -z "${url:-}" ]; then
  warn "the latest release carries no ${arch} .deb; keeping the current index"
  exit 0
fi

deb=${url##*/}

if [ ! -f "$DIR/$deb" ]; then
  tmp="$DIR/.$deb.part"
  if ! curl "$@" -o "$tmp" "$url"; then
    rm -f "$tmp"
    warn "download of $deb failed; keeping the current index"
    exit 0
  fi
  # Refuse to index something that is not a package.
  if ! dpkg-deb --info "$tmp" >/dev/null 2>&1; then
    rm -f "$tmp"
    warn "$deb is not a valid .deb; keeping the current index"
    exit 0
  fi
  mv "$tmp" "$DIR/$deb"
fi

# One package in the repository: drop every other .deb and any stale part.
for f in "$DIR"/*.deb "$DIR"/.*.part; do
  [ -e "$f" ] || continue
  [ "$f" = "$DIR/$deb" ] || rm -f "$f"
done

if ! (cd "$DIR" && dpkg-scanpackages --multiversion . 2>/dev/null >Packages.new); then
  rm -f "$DIR/Packages.new"
  warn "could not build the index; keeping the current one"
  exit 0
fi
mv "$DIR/Packages.new" "$DIR/Packages"
BODY

chmod 0755 "$SYNC"

# --- the apt source --------------------------------------------------
# A flat repository on local disk that only root can write. Unsigned, so
# marked trusted: what is trusted is the HTTPS fetch from GitHub above.
cat > "$SOURCE" <<SOURCES
Types: deb
URIs: file:${DIR}
Suites: ./
Trusted: yes
SOURCES
chmod 0644 "$SOURCE"

# --- the hook --------------------------------------------------------
# apt offers no hook before `upgrade` resolves versions, only before
# `update` — so this is where the refresh goes. It must never be able to
# break apt, hence the `|| true`.
cat > "$HOOK" <<HOOKCONF
// Refresh the local homedash repository before apt reads package lists.
// Installed by hub/packaging/apt-repo.sh.
APT::Update::Pre-Invoke { "${SYNC} || true"; };
HOOKCONF
chmod 0644 "$HOOK"

# --- first run -------------------------------------------------------
"$SYNC"
apt-get update

echo
echo "apt-repo: done. HomeDash now upgrades with the rest of the machine:"
echo "    sudo apt update && sudo apt upgrade"
if command -v apt-cache >/dev/null 2>&1; then
  echo
  apt-cache policy homedash || true
fi
