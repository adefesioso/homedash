# The omp-only slice of layout.sh: just the binary, nothing else. Runs as
# root, is idempotent, and is what UpdateOmp sends instead of the whole
# layout, so "update omp on the network" doesn't also touch accounts, the
# key, or the cage.
[ "$(id -u)" = 0 ] || { echo "must run as root" >&2; exit 1; }
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
