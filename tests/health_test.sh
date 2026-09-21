# docs/running/README.md#is-the-hub-well — the hub read the way a remote
# is: what its machine reports about itself now, and one line per thing
# it has to keep running, each ok/warn/bad/off with the reason beside it.

_health() { hub_curl hub-a "$(token_a)" GET /api/hub/health; }

test_the_hub_reports_its_own_machine_and_state_file() {
  local body status
  body=$(_health)
  status=$(echo "$body" | hub_status)
  assert_status "GET /api/hub/health answers" "$status" 200
  body=$(echo "$body" | hub_body)
  # The machine's own facts, the same fields a remote's facts.sh prints.
  assert_eq "the hostname is the machine's own" "$(echo "$body" | jq -r .machine.hostname)" "$("$SSH" hub-a hostname 2>/dev/null)"
  assert_true "cores, memory and uptime are read from the box" test "$(echo "$body" | jq '.machine.cores > 0 and .machine.memTotal > 0 and .machine.uptime > 0')" = true
  assert_true "the state file's disk is sized" test "$(echo "$body" | jq '.stateDir.diskSize > 0 and .stateDir.dbSize > 0')" = true
  assert_eq "the state directory is where the state file lives" "$(echo "$body" | jq -r .stateDir.path)" "$(hub_curl hub-a "$(token_a)" GET /api/hub | hub_body | jq -r '.statePath | sub("/[^/]+$"; "")')"
}

test_every_line_has_a_state_and_a_reason() {
  local body names
  body=$(_health | hub_body)
  names=$(echo "$body" | jq -r '.checks[].name' | sort | tr '\n' ' ')
  assert_eq "one line per thing the hub keeps running" "$names" "Agent Credential vault Export Hosts Load Memory Space State disk Tasks "
  assert_eq "every line is ok, warn, bad or off" "$(echo "$body" | jq -r '[.checks[].state] | map(select(. != "ok" and . != "warn" and . != "bad" and . != "off")) | length')" 0
  assert_eq "every line says why" "$(echo "$body" | jq -r '[.checks[] | select(.detail == "")] | length')" 0
  # The page's own state is the worst of its lines.
  assert_eq "the whole is the worst line" "$(echo "$body" | jq -r '.state')" \
    "$(echo "$body" | jq -r 'if any(.checks[]; .state == "bad") then "bad" elif any(.checks[]; .state == "warn") then "warn" else "ok" end')"
}

test_the_hosts_line_counts_what_the_hosts_tab_shows() {
  local body hosts online
  body=$(_health | hub_body)
  hosts=$(hub_curl hub-a "$(token_a)" GET /api/hosts | hub_body)
  online=$(echo "$hosts" | jq '[.[] | select(.status == "online")] | length')
  if [ "$(echo "$hosts" | jq length)" = 0 ]; then
    assert_eq "no hosts is off, not bad" "$(echo "$body" | jq -r '.checks[] | select(.name == "Hosts") | .state')" off
  else
    assert_eq "the Hosts line counts the online machines" "$(echo "$body" | jq -r '.checks[] | select(.name == "Hosts") | .detail')" "$online of $(echo "$hosts" | jq length) online"
  fi
}

test_health_is_behind_sign_in() {
  local status
  status=$(hub_curl hub-a "" GET /api/hub/health | hub_status)
  assert_status "without an account /api/hub/health is 401" "$status" 401
}
