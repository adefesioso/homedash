# docs/pooling/hosts.md
#
# The lab's remotes are already enrolled by lab/vms.sh + the panel's
# enrollment flow the first time the lab was brought up; these tests read
# that state rather than re-enrolling, so they're safe to run repeatedly.
# Enrollment itself is exercised up to the pasted line: a code is minted,
# the script it fetches is served once, and a code nobody minted is not.

_host() { hub_curl hub-a "$(token_a)" GET /api/hosts | hub_body | jq -c --arg n "$1" '.[] | select(.name==$n)'; }

test_enrolled_hosts_are_listed() {
  local tok out status body names expect
  tok=$(token_a)
  out=$(hub_curl hub-a "$tok" GET /api/hosts)
  status=$(echo "$out" | hub_status)
  body=$(echo "$out" | hub_body)
  assert_status "GET /api/hosts on hub-a" "$status" 200
  names=$(echo "$body" | jq -r '.[].name' 2>/dev/null | sort | tr '\n' ' ')
  for expect in remote-small remote-mid remote-big; do
    case " $names " in
      *" $expect "*) pass "hub-a's fleet includes $expect" ;;
      *) fail "hub-a's fleet includes $expect (got: $names)" ;;
    esac
  done
}

test_new_remote_mints_a_single_use_code_and_one_pasted_line() {
  # hosts.md#joining-a-machine: "paste one line into that machine's
  # terminal. The line carries a single-use code".
  local tok body code line status
  tok=$(token_a)
  body=$(hub_curl hub-a "$tok" POST /api/hosts/enroll '{"name":"tests-enroll"}' | hub_body)
  code=$(echo "$body" | jq -r .code)
  line=$(echo "$body" | jq -r .line)
  assert_true "a code was minted" test -n "$code" -a "$code" != "null"
  case "$line" in
    *"/enroll/$code"*"| sudo sh"*) pass "the line fetches the script by that code and pipes it to sh" ;;
    *) fail "the line fetches the script by that code (got: $line)" ;;
  esac
  # The script is served to the code, unauthenticated (the new machine
  # has no token yet) on the enrollment listener — its own port, apart
  # from the panel and the API — and refused to a code nobody minted.
  local eport
  eport=$(echo "$line" | sed -E 's#.*://[^/:]+:([0-9]+)/enroll/.*#\1#')
  status=$("$SSH" hub-a "curl -sS -o /dev/null -w '%{http_code}' http://127.0.0.1:${eport:-7434}/enroll/$code")
  assert_status "GET /enroll/{code} serves the enrollment script" "$status" 200
  status=$("$SSH" hub-a "curl -sS -o /dev/null -w '%{http_code}' http://127.0.0.1:${eport:-7434}/enroll/not-a-code")
  assert_status "an unminted code gets nothing" "$status" 404
  # A pending code is not a host: the fleet is what enrolled, not what
  # was invited.
  assert_eq "a minted code is not yet a host record" "$(_host tests-enroll)" ""
}

test_new_remote_can_carry_a_rebuild_script() {
  # rebuild.md: "New remote can carry one. Pick a host — one that is
  # failing, or gone".
  local tok status
  tok=$(token_a)
  status=$(hub_curl hub-a "$tok" POST /api/hosts/enroll '{"name":"tests-enroll-2","rebuildFrom":"remote-mid"}' | hub_status)
  assert_status "an enrollment that rebuilds from remote-mid is accepted" "$status" 200
  status=$(hub_curl hub-a "$tok" POST /api/hosts/enroll '{"name":"tests-enroll-3","rebuildFrom":"no-such-host"}' | hub_status)
  assert_status "rebuilding from a host that does not exist is refused" "$status" 400
}

test_heartbeat_reports_online() {
  local s
  s=$(_host remote-big | jq -r .status)
  assert_eq "remote-big's card reads Online after the heartbeat" "$s" "online"
  assert_true "the card says when it was last seen" test -n "$(_host remote-big | jq -r '.lastSeen // empty')"
}

test_heartbeat_pins_the_host_key_and_rereads_the_specs() {
  # hosts.md: "the hub learns which host key to expect"; each sweep
  # "re-reads what the remote's agent says about itself: its version, the
  # model it is set to".
  local h
  h=$(_host remote-mid)
  assert_true "remote-mid's record pins a host key" test -n "$(echo "$h" | jq -r '.hostKey // empty')"
  assert_true "the facts carry the machine's own hostname" test "$(echo "$h" | jq -r '.facts.hostname')" = "remote-mid"
  assert_true "the facts carry the agent's reported version" test -n "$(echo "$h" | jq -r '.facts.agent.version // empty')"
}

test_pi_class_box_still_enrolls() {
  # docs/pooling/hosts.md: "A Pi-class box ... cannot run the coding agent
  # at all; it still enrolls, still pools" — remote-small is 1 GB.
  local h
  h=$(_host remote-small)
  assert_eq "remote-small (Pi-class, 1 GB) is enrolled and online" "$(echo "$h" | jq -r .status)" "online"
  assert_true "its memory is under the agent's floor, which is what the card reads" \
    test "$(echo "$h" | jq -r '.facts.memTotal')" -lt $((1536*1024*1024))
}

test_each_sweep_keeps_a_handful_of_numbers() {
  # hosts.md#watching-it-change: "Each sweep keeps a handful of numbers —
  # free space per mountpoint, memory in use, load".
  local tok body n
  tok=$(token_a)
  body=$(hub_curl hub-a "$tok" GET "/api/hosts/remote-big/metrics?hours=24" | hub_body)
  n=$(echo "$body" | jq 'length')
  assert_true "remote-big has readings over the last day" test "$n" -gt 0
  assert_true "a reading carries memory in use and total" \
    bash -c "echo '$body' | jq -e '.[0] | .memTotal > 0 and .memUsed >= 0' >/dev/null"
  assert_true "a reading carries free space per mountpoint" \
    bash -c "echo '$body' | jq -e '.[0].mounts | has(\"/\")' >/dev/null"
}

test_unknown_host_is_refused_not_silently_empty() {
  local tok status
  tok=$(token_a)
  status=$(hub_curl hub-a "$tok" GET "/api/hosts/does-not-exist" | hub_status)
  assert_status "GET /api/hosts/{unknown} is refused, not silently empty" "$status" 404
}

test_locking_shuts_the_front_door_and_the_hub_still_gets_in() {
  # hosts.md#shutting-the-front-door: "Locked, the machine answers only
  # to the hub's account — password logins, root logins and every other
  # account refused." The lab reaches guests as `debian` with the lab
  # key; locked, that login must fail while the hub's own path still
  # works, and the card shows what the machine reports, not what the hub
  # asked for.
  local tok status
  tok=$(token_a)
  status=$(hub_curl hub-a "$tok" POST /api/hosts/remote-small/lock '{"locked":true}' | hub_status)
  assert_status "locking remote-small succeeds" "$status" 204
  trap 'hub_curl hub-a "$tok" POST /api/hosts/remote-small/lock "{\"locked\":false}" >/dev/null' RETURN

  if "$SSH" remote-small -o ConnectTimeout=10 true 2>/dev/null; then
    fail "the lab's debian login is refused while locked"
  else
    pass "the lab's debian login is refused while locked"
  fi
  status=$(hub_curl hub-a "$tok" POST /api/hosts/remote-small/run '{"command":"id -un","timeoutSeconds":10}' | hub_status)
  assert_status "the hub's own account still gets in" "$status" 200
  _reports_locked() { [ "$(_host remote-small | jq -r '.facts.lock.locked')" = "true" ]; }
  assert_true "the card shows what the machine reports: locked" wait_until 30 _reports_locked

  status=$(hub_curl hub-a "$tok" POST /api/hosts/remote-small/lock '{"locked":false}' | hub_status)
  assert_status "unlocking remote-small succeeds" "$status" 204
  trap - RETURN
  _debian_back() { "$SSH" remote-small -o ConnectTimeout=10 true 2>/dev/null; }
  assert_true "the debian login works again once unlocked" wait_until 30 _debian_back
  _reports_unlocked() { [ "$(_host remote-small | jq -r '.facts.lock.locked')" = "false" ]; }
  assert_true "the card shows what the machine reports: unlocked" wait_until 30 _reports_unlocked
}

test_lock_and_unlock_are_events_and_land_in_the_rebuild_script() {
  # hosts.md: events record the transition; rebuild.md: what the panel
  # does to a remote is appended on its own.
  local tok
  tok=$(token_a)
  assert_true "locking was recorded as an event" test "$(event_count hub-a "$tok" host.locked remote-small)" -gt 0
  assert_true "unlocking was recorded as an event" test "$(event_count hub-a "$tok" host.unlocked remote-small)" -gt 0
  case "$(_host remote-small | jq -r .rebuildScript)" in
    *"lock direct SSH access"*) pass "the lock is in remote-small's rebuild script" ;;
    *) fail "the lock is in remote-small's rebuild script" ;;
  esac
}

test_rebuild_script_save_round_trips_byte_exact() {
  # H-10: the panel used to JSON.stringify the script before PUTting it,
  # so a save quoted the whole thing and turned every real newline into
  # a literal \n. hosts.md: "the panel and `homedash rebuild` edit the
  # same text, so a save from either is byte-exact" — a JSON
  # {"script": …} body (the panel) and a raw text/plain body (the CLI)
  # must both round-trip the script exactly, CRLF, quotes, a literal
  # \n and a real newline included.
  local tok before torture got status body
  tok=$(token_a)
  before=$(hub_curl hub-a "$tok" GET /api/hosts/remote-big | hub_body | jq -r .rebuildScript)
  torture=$(printf 'CRLF here\r\nquote "in here"\r\nliteral \\n not a newline\nreal newline above')

  body=$(jq -nc --arg s "$torture" '{script: $s}')
  status=$(hub_curl hub-a "$tok" PUT /api/hosts/remote-big/rebuild-script "$body" | hub_status)
  assert_status "PUT {\"script\"} accepts the torture string" "$status" 204
  got=$(hub_curl hub-a "$tok" GET /api/hosts/remote-big | hub_body | jq -r .rebuildScript)
  assert_eq "the JSON save round-trips byte-exact" "$got" "$torture"

  status=$("$SSH" hub-a "curl -sS -o /dev/null -w '%{http_code}' -X PUT -H 'Authorization: Bearer $tok' \
    --data-binary @- http://127.0.0.1:7433/api/hosts/remote-big/rebuild-script" <<<"$torture")
  assert_status "PUT text/plain accepts the torture string" "$status" 204
  got=$(hub_curl hub-a "$tok" GET /api/hosts/remote-big | hub_body | jq -r .rebuildScript)
  assert_eq "the raw-body save round-trips byte-exact" "$got" "$torture"

  status=$(hub_curl hub-a "$tok" PUT /api/hosts/remote-big/rebuild-script '"a bare json string"' | hub_status)
  assert_status "a bare JSON string is refused" "$status" 400

  "$SSH" hub-a "curl -sS -o /dev/null -X PUT -H 'Authorization: Bearer $tok' \
    --data-binary @- http://127.0.0.1:7433/api/hosts/remote-big/rebuild-script" <<<"$before"
  got=$(hub_curl hub-a "$tok" GET /api/hosts/remote-big | hub_body | jq -r .rebuildScript)
  assert_eq "remote-big's original script is restored" "$got" "$before"
}

test_credentials_revoke_shows_on_the_card() {
  # H-9 / credentials.md: Revoke used to leave the card reading a stale
  # age with nothing saying the credentials were actually revoked.
  # credentialsRevokedAt is set by /credentials/revoke and cleared by
  # /credentials; ends on remote-big with fresh credentials either way.
  local tok status revoked cleared
  tok=$(token_a)
  status=$(hub_curl hub-a "$tok" POST /api/hosts/remote-big/credentials/revoke | hub_status)
  assert_status "Revoke succeeds" "$status" 204
  revoked=$(_host remote-big | jq -r '.credentialsRevokedAt // empty')
  assert_true "GET /api/hosts shows the revoked state after Revoke" test -n "$revoked"

  status=$(hub_curl hub-a "$tok" POST /api/hosts/remote-big/credentials | hub_status)
  assert_status "Update credentials succeeds" "$status" 204
  cleared=$(_host remote-big | jq -r '.credentialsRevokedAt // empty')
  assert_eq "the revoked state is cleared after Update credentials" "$cleared" ""
}

test_raw_disks_are_listed_as_unformatted() {
  # S-1 / storage.md: a whole-disk block device with no filesystem of its
  # own and no partition that has one is raw — nothing for df to see, so
  # facts.sh's `disks` (lsblk -J) is the only way the panel learns it
  # exists at all. The lab deliberately leaves two spare disks raw on
  # remote-big; do not format them.
  local h raw
  h=$(_host remote-big)
  raw=$(echo "$h" | jq -r '[.facts.disks.blockdevices[]? | select(.type=="disk" and (.fstype // "")=="" and ((.children // []) | length)==0) | .name] | sort | join(",")')
  assert_eq "GET /api/hosts lists remote-big's raw disks as unformatted" "$raw" "sda,sdc"
}

test_no_homedash_agent_on_remote() {
  # docs/pooling/hosts.md: "There is no HomeDash agent on a remote ...
  # the only HomeDash code on a remote is a fact-collection script the
  # heartbeat pipes in fresh every time, plus two three-line helpers".
  local out
  out=$("$SSH" remote-big "which homedash 2>/dev/null; echo done" 2>/dev/null)
  case "$out" in
    *"/homedash"*) fail "no 'homedash' binary installed on remote-big" ;;
    *) pass "no 'homedash' binary installed on remote-big" ;;
  esac
  # A job runs in a transient unit the hub starts for it, and the agent's
  # cage is a oneshot that loads an nftables rule at boot; nothing of
  # HomeDash's stays running or listens on a remote.
  out=$("$SSH" remote-big "systemctl list-units --type=service --state=running 2>/dev/null | grep -i homedash; sudo ss -ltnp 2>/dev/null | grep -i homedash; echo done" 2>/dev/null)
  case "$out" in
    *"homedash"*) fail "nothing of HomeDash's runs or listens on a remote (got: $out)" ;;
    *) pass "nothing of HomeDash's runs or listens on a remote; the hub's channel is SSH" ;;
  esac
}

# The lab keeps one remote on each network manager the feature writes
# for (lab/README.md): remote-mid on netplan, remote-small on ifupdown,
# remote-big on NetworkManager. Each is exercised the same way.
_address_managers="remote-mid:10.1.0.12:netplan remote-small:10.1.0.11:ifupdown remote-big:10.1.0.13:nm"

test_holding_an_address_is_confirmed_from_the_new_address() {
  # hosts.md#holding-an-address: "Confirmed, the hub re-pins the address
  # and the card follows." Each remote moves to 10.1.0.42 and back,
  # through the panel's door, and the hub's record follows each time.
  # The machine's word: eth0 carries exactly the held address.
  local tok status body addr row host orig mgr
  tok=$(token_a)
  status=$(hub_curl hub-a "$tok" PUT /api/hosts/remote-mid/address '{"interface":"eth0","address":"10.1.0.300/24"}' | hub_status)
  assert_status "an address that is not one is refused before anything reaches the machine" "$status" 400
  status=$(hub_curl hub-a "$tok" PUT /api/hosts/remote-mid/address '{"interface":"eth0","address":"10.1.0.42/24","gateway":"10.2.0.1"}' | hub_status)
  assert_status "a gateway outside the prefix is refused" "$status" 400

  for row in $_address_managers; do
    IFS=: read -r host orig mgr <<<"$row"
    _hold() { hub_curl hub-a "$tok" PUT "/api/hosts/$host/address" "{\"interface\":\"eth0\",\"address\":\"$1/24\",\"gateway\":\"10.1.0.1\",\"dns\":[\"10.20.30.1\"]}"; }
    trap '_hold "$orig" >/dev/null' RETURN
    assert_eq "$host's facts name the manager the lab put it on" "$(_host "$host" | jq -r '.facts.network.manager')" "$mgr"
    body=$(_hold 10.1.0.42)
    assert_status "holding $host's eth0 at 10.1.0.42 is confirmed ($mgr)" "$(echo "$body" | hub_status)" 200
    assert_eq "the hub re-pinned the address it reaches $host at" "$(_host "$host" | jq -r .addr)" "10.1.0.42"
    addr=$(hub_curl hub-a "$tok" POST "/api/hosts/$host/run" '{"command":"ip -o -4 addr show eth0 | awk \"{print \\$4}\" | tr \"\\n\" \" \"","timeoutSeconds":10}' | hub_body | jq -r .stdout)
    assert_eq "$host's eth0 carries exactly the held address, not the old one beside it" "$(echo $addr)" "10.1.0.42/24"
    _held() { [ "$(_host "$host" | jq -r '[.facts.interfaces[] | select(.name=="eth0") | .addrs[].cidr] | join(",")')" = "10.1.0.42/24" ]; }
    assert_true "the card shows what $host reports: eth0 held at 10.1.0.42/24" wait_until 90 _held
    _in_script() { _host "$host" | jq -r .rebuildScript | grep -q 'eth0 held at 10.1.0.42/24'; }
    assert_true "the change lands in $host's rebuild script" _in_script

    status=$(_hold "$orig" | hub_status)
    assert_status "holding $host back at $orig is confirmed" "$status" 200
    assert_eq "the record follows again" "$(_host "$host" | jq -r .addr)" "$orig"
    trap - RETURN
  done
}

test_an_address_the_hub_cannot_reach_reverts_on_its_own() {
  # hosts.md#holding-an-address: "Not confirmed — the hub could not reach
  # it ... the machine puts its previous config back on its own, and the
  # card says the change was refused." Takes the guard's full window,
  # once per manager. Back to DHCP is the same path here: the lab's
  # bridges have no DHCP server, so no lease comes and the held address
  # must come back — on every manager, including the one (ifupdown)
  # whose ifdown would otherwise leave the old address in place.
  local tok before status row host orig mgr
  tok=$(token_a)
  for row in $_address_managers; do
    IFS=: read -r host orig mgr <<<"$row"
    before=$(event_count hub-a "$tok" host.address_refused "$host")
    status=$(hub_curl hub-a "$tok" PUT "/api/hosts/$host/address" '{"interface":"eth0","address":"10.9.9.9/24","gateway":"10.9.9.1"}' | hub_status)
    assert_status "an address on a subnet the hub cannot reach is not confirmed ($host, $mgr)" "$status" 502
    assert_eq "the hub's record is unchanged" "$(_host "$host" | jq -r .addr)" "$orig"
    assert_true "the refusal is an event" test "$(event_count hub-a "$tok" host.address_refused "$host")" -gt "$before"
    _back() { [ "$(hub_curl hub-a "$tok" POST "/api/hosts/$host/run" '{"command":"ip -o -4 addr show eth0 | awk \"{print \\$4}\"","timeoutSeconds":10}' | hub_body | jq -r .stdout 2>/dev/null)" = "$orig/24" ]; }
    assert_true "$host put its previous address back on its own" wait_until 60 _back
    status=$(hub_curl hub-a "$tok" PUT "/api/hosts/$host/address" '{"interface":"eth0","address":""}' | hub_status)
    assert_status "back to DHCP with no lease to be had is not confirmed ($host, $mgr)" "$status" 502
    assert_true "$host kept its held address" wait_until 60 _back
  done
}
