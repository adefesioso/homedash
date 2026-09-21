#!/bin/bash
# The acceptance suite: verifies claims made in ../README.md and ../docs
# against a live lab (../lab). Run `make -C ../lab up` first, and
# `make -C ../lab deploy` to put the current build on both hubs.
#
# Usage:
#   ./run.sh              # every test file
#   ./run.sh hosts safety # just tests/hosts_test.sh and tests/safety_test.sh
set -uo pipefail
cd "$(dirname "$0")"
. ./lib.sh

# A token name is unique now (C-6): `homedash token NAME` replaces
# rather than duplicates, so at most one "tests-suite" row exists at any
# time, however many times token_a/token_b minted it over the run.
# Minting one more and deleting by name sweeps that one row (zero is
# fine too, if nothing ever needed a fresh one).
cleanup() {
  local t
  t=$(hub_token hub-a); [ -n "$t" ] && hub_curl hub-a "$t" DELETE /api/tokens/tests-suite >/dev/null 2>&1
  t=$(hub_token hub-b); [ -n "$t" ] && hub_curl hub-b "$t" DELETE /api/tokens/tests-suite >/dev/null 2>&1
}
trap cleanup EXIT

# Mint the two tokens once here, in this shell, so every `$(token_a)`
# in a subshell finds them cached instead of minting another row.
HUB_A_TOK=$(hub_token hub-a)
HUB_B_TOK=$(hub_token hub-b)

want=("$@")
for f in *_test.sh; do
  name=${f%_test.sh}
  if [ "${#want[@]}" -gt 0 ]; then
    keep=false
    for w in "${want[@]}"; do [ "$w" = "$name" ] && keep=true; done
    $keep || continue
  fi
  run_test_file "./$f"
done

echo ""
echo "== summary =="
printf '%d passed, %d failed, %d skipped\n' "$PASS" "$FAIL" "$SKIP"
[ "$FAIL" -eq 0 ]
