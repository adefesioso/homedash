# docs/running/cli.md — the same binary as a CLI, one subcommand per
# thing the panel does, every one taking --json and printing exactly
# what the hub's API returned. `homedash login` (the browser hand-off)
# is in tests/ui/cli.spec.js; here the CLI is signed in the way a script
# is — an API token — and run on hub-a itself against its own hub, so
# what is under test is the subcommands, not the route in.

_cli() { # _cli <token> <args...> -> stdout
  local tok=$1; shift
  "$SSH" hub-a "HOMEDASH_HUB=http://127.0.0.1:7433 HOMEDASH_TOKEN=$tok homedash $*" 2>/dev/null
}
_cli_rc() { # _cli_rc <token> <args...> -> exit code
  local tok=$1; shift
  "$SSH" hub-a "HOMEDASH_HUB=http://127.0.0.1:7433 HOMEDASH_TOKEN=$tok homedash $* >/dev/null 2>&1; echo \$?" 2>/dev/null
}

test_whoami_says_which_hub_as_whom() {
  local out
  out=$(_cli "$(token_a)" whoami)
  case "$out" in
    *"127.0.0.1:7433"*"API token"*) pass "homedash whoami names the hub and the token sign-in" ;;
    *) fail "homedash whoami names the hub and the token sign-in (got: $out)" ;;
  esac
}

test_every_read_subcommand_prints_what_the_api_returned() {
  # "Every subcommand takes --json and prints exactly what the hub's API
  # returned." Each one is compared to the endpoint it fronts.
  local tok
  tok=$(token_a)
  assert_eq "homedash hosts --json is GET /api/hosts" \
    "$(_cli "$tok" hosts --json | jq -S -c '[.[].name]')" \
    "$(hub_curl hub-a "$tok" GET /api/hosts | hub_body | jq -S -c '[.[].name]')"
  assert_eq "homedash devices --json is GET /api/devices" \
    "$(_cli "$tok" devices --json | jq -S -c '[.devices[].id]')" \
    "$(hub_curl hub-a "$tok" GET /api/devices | hub_body | jq -S -c '[.devices[].id]')"
  assert_eq "homedash storage --json is GET /api/clusters" \
    "$(_cli "$tok" storage --json | jq -S -c '[.. | .name? // empty] | sort')" \
    "$(hub_curl hub-a "$tok" GET /api/clusters | hub_body | jq -S -c '[.. | .name? // empty] | sort')"
  assert_eq "homedash catalog --json is GET /api/apps/catalog" \
    "$(_cli "$tok" catalog --json | jq -S -c '[.[].name]')" \
    "$(hub_curl hub-a "$tok" GET /api/apps/catalog | hub_body | jq -S -c '[.[].name]')"
  assert_eq "homedash secrets --json is GET /api/secrets" \
    "$(_cli "$tok" secrets --json | jq -S -c .)" \
    "$(hub_curl hub-a "$tok" GET /api/secrets | hub_body | jq -S -c .)"
  assert_eq "homedash events --json is GET /api/events" \
    "$(_cli "$tok" events --json | jq -S -c '[.[].id] | sort | .[0:50]')" \
    "$(hub_curl hub-a "$tok" GET /api/events | hub_body | jq -S -c '[.[].id] | sort | .[0:50]')"
  assert_eq "homedash jobs remote-mid --json is GET /api/jobs?host=remote-mid" \
    "$(_cli "$tok" jobs remote-mid --json | jq -S -c '[.[].id]')" \
    "$(hub_curl hub-a "$tok" GET '/api/jobs?host=remote-mid' | hub_body | jq -S -c '[.[].id]')"
  assert_eq "homedash place is POST /api/apps/placement" \
    "$(_cli "$tok" place "'{\"cores\":1,\"memoryMB\":512,\"diskGB\":1}'" --json | jq -S -c '[.[].host]')" \
    "$(hub_curl hub-a "$tok" POST /api/apps/placement '{"cores":1,"memoryMB":512,"diskGB":1}' | hub_body | jq -S -c '[.[].host]')"
  assert_true "homedash apps --json lists stacks live from the remotes" \
    bash -c "$(printf '%q' "$SSH") hub-a 'HOMEDASH_HUB=http://127.0.0.1:7433 HOMEDASH_TOKEN=$tok homedash apps --json' 2>/dev/null | jq -e 'has(\"stacks\")' >/dev/null"
}

test_run_write_and_rebuild_reach_the_fleet_through_the_same_gate() {
  local tok out
  tok=$(token_a)
  out=$(_cli "$tok" run remote-mid -- echo cli-ran-here)
  case "$out" in
    *cli-ran-here*) pass "homedash run executes one command on one host" ;;
    *) fail "homedash run executes one command on one host (got: $out)" ;;
  esac
  assert_eq "homedash run of a refused command is refused" "$(_cli_rc "$tok" run remote-mid -- reboot)" "1"

  "$SSH" hub-a "HOMEDASH_HUB=http://127.0.0.1:7433 HOMEDASH_TOKEN=$tok homedash write remote-mid /tmp/tests-cli-file" <<<"written by the cli" >/dev/null 2>&1
  out=$(_cli "$tok" run remote-mid -- cat /tmp/tests-cli-file)
  assert_eq "homedash write puts a file on a host from stdin" "$out" "written by the cli"
  _cli "$tok" run remote-mid -- rm -f /tmp/tests-cli-file >/dev/null

  # `rebuild HOST` with stdin piped is a replace, and stdin over a plain
  # ssh is a pipe; ask for a terminal so the read form is what runs.
  out=$("$SSH" hub-a -o RequestTTY=force "HOMEDASH_HUB=http://127.0.0.1:7433 HOMEDASH_TOKEN=$tok homedash rebuild remote-mid" 2>/dev/null | tr -d '\r')
  assert_eq "homedash rebuild reads the host's rebuild script" \
    "$(echo "$out" | head -c 40)" \
    "$(hub_curl hub-a "$tok" GET /api/hosts/remote-mid | hub_body | jq -r .rebuildScript | head -c 40)"
}

test_token_name_replaces_the_previous_one() {
  # AGENTS.md: "homedash token NAME revokes any existing token of that
  # name and mints a new one" — a name is one live token (C-6); this is
  # what lets tests/lib.sh mint "tests-suite" over and over.
  local first second status
  first=$("$SSH" hub-a "sudo -u homedash homedash token tests-cli-replace" 2>/dev/null)
  second=$("$SSH" hub-a "sudo -u homedash homedash token tests-cli-replace" 2>/dev/null)
  assert_true "minting the same name twice gives two different tokens" test "$first" != "$second"
  status=$(hub_curl hub-a "$first" GET /api/hosts | hub_status)
  assert_status "the first token is revoked once its name is reused" "$status" 401
  status=$(hub_curl hub-a "$second" GET /api/hosts | hub_status)
  assert_status "the replacement token works" "$status" 200
  hub_curl hub-a "$second" DELETE /api/tokens/tests-cli-replace >/dev/null
}

test_a_viewers_cli_reads_and_an_admins_does_everything() {
  # cli.md: "A viewer's CLI reads; an admin's does everything below."
  local admin_tok viewer_tok
  admin_tok=$(token_a)
  viewer_tok=$(hub_curl hub-a "$admin_tok" POST /api/tokens '{"name":"tests-cli-viewer","role":"viewer"}' | hub_body | jq -r .token)
  assert_eq "a viewer's homedash hosts works" "$(_cli_rc "$viewer_tok" hosts)" "0"
  assert_ne "a viewer's homedash run is refused" "$(_cli_rc "$viewer_tok" run remote-mid -- id)" "0"
  hub_curl hub-a "$admin_tok" DELETE /api/tokens/tests-cli-viewer >/dev/null
}

test_the_cli_is_not_signed_in_without_a_session_or_token() {
  local out
  out=$("$SSH" hub-a "env -u HOMEDASH_TOKEN -u HOMEDASH_HUB HOME=/tmp/tests-nohome homedash hosts 2>&1; echo rc=\$?" 2>/dev/null)
  case "$out" in
    *"not signed in"*"rc=1"*) pass "with no session and no token the CLI says to log in" ;;
    *) fail "with no session and no token the CLI says to log in (got: $out)" ;;
  esac
}

test_an_unknown_api_path_is_404_not_the_panel() {
  # docs/running/README.md: "an unknown /api path is a 404, not the
  # panel." Before this, every unmatched /api/* path fell through to the
  # SPA's index.html with a 200 (C-4), which api.js would try to parse
  # as JSON.
  local tok resp status body
  tok=$(token_a)
  resp=$(hub_curl hub-a "$tok" GET /api/nope)
  status=$(echo "$resp" | hub_status)
  body=$(echo "$resp" | hub_body)
  assert_status "GET /api/nope is a 404" "$status" 404
  case "$body" in
    *'<'*) fail "the 404 body is not HTML (got: $body)" ;;
    *) pass "the 404 body is not HTML" ;;
  esac
}
