# docs/pooling/network.md — the remotes scan, the hub merges; an enrolled
# remote seen by another is not a device; your name outlives the guess.

_hosts() { hub_curl hub-a "$(token_a)" GET /api/hosts | hub_body; }

test_scan_now_records_what_each_remote_can_see() {
  local tok body scanners online
  tok=$(token_a)
  body=$(hub_curl hub-a "$tok" POST /api/devices/scan | hub_body)
  scanners=$(echo "$body" | jq '.scanners | length')
  online=$(_hosts | jq '[.[] | select(.status=="online")] | length')
  assert_eq "every online remote has a scan outcome" "$scanners" "$online"
  assert_true "each scanned remote saw its LAN with no error" \
    bash -c "echo '$body' | jq -e '[.scanners[] | select(.lan and .error==\"\")] | length == $online' >/dev/null"
  # The lab's VMs share one bridge, so each remote sees the others and
  # the hub — and none of them may appear as a device (N-x: the hub is
  # not a device either, docs/pooling/network.md).
  local devices own hub_macs
  devices=$(echo "$body" | jq -r '.devices[] | select(.kind=="lan") | .addr' | sort)
  own=$(_hosts | jq -r '.[].facts.interfaces[]?.mac' | sort)
  assert_true "an enrolled remote is never listed as a device" \
    bash -c "! comm -12 <(echo '$devices') <(echo '$own') | grep -q ."
  hub_macs=$("$SSH" hub-a "ip -o link show 2>/dev/null | awk '\$2!=\"lo:\" && /link\\/ether/ { for(i=1;i<=NF;i++) if(\$i==\"link/ether\") print tolower(\$(i+1)) }'" | sort -u)
  assert_true "the hub itself is never listed as a device" \
    bash -c "! comm -12 <(echo '$devices') <(echo '$hub_macs') | grep -q ."
  assert_true "the remotes saw at least one neighbour" \
    bash -c "echo '$body' | jq -e '[.devices[] | select(.kind==\"lan\")] | length > 0' >/dev/null"
}

test_name_a_device_and_the_guess_stays() {
  local tok dev id guess named
  tok=$(token_a)
  dev=$(hub_curl hub-a "$tok" GET /api/devices | hub_body | jq -c '.devices[0] // empty')
  if [ -z "$dev" ]; then skip "no device to name (scan first)"; return; fi
  id=$(echo "$dev" | jq -r .id)
  guess=$(echo "$dev" | jq -r .guessName)
  named=$(hub_curl hub-a "$tok" PUT "/api/devices/$id" '{"name":"tests-thing","kind":"appliance"}' | hub_body)
  assert_eq "the name is yours" "$(echo "$named" | jq -r .name)" "tests-thing"
  assert_eq "the kind is yours" "$(echo "$named" | jq -r .userKind)" "appliance"
  assert_eq "the guess is untouched beside it" "$(echo "$named" | jq -r .guessName)" "$guess"
  hub_curl hub-a "$tok" PUT "/api/devices/$id" '{"name":"","kind":""}' >/dev/null
}

test_devices_never_reach_a_peer() {
  # README's table: Network crosses the space "Never". Hub-b's view of
  # hub-a as a peer carries offers and services, and no device.
  local peers
  peers=$(hub_curl hub-b "$(token_b)" GET /api/peers | hub_body)
  case "$peers" in
    *evice*) fail "nothing about devices is in a peer's view of this hub" ;;
    *) pass "nothing about devices is in a peer's view of this hub" ;;
  esac
}

test_scan_interval_and_forget_days_are_settings() {
  # network.md: "Every network.scan_minutes (ten by default; zero
  # switches scanning off)" and "a device nobody has seen for
  # network.forget_days (thirty by default) leaves the list".
  local tok got
  tok=$(token_a)
  hub_curl hub-a "$tok" PUT /api/settings '{"network.scan_minutes":"0","network.forget_days":"7"}' >/dev/null
  got=$(hub_curl hub-a "$tok" GET /api/settings | hub_body | jq -r '[."network.scan_minutes", ."network.forget_days"] | join(" ")')
  assert_eq "scan_minutes and forget_days are stored and read back" "$got" "0 7"
  hub_curl hub-a "$tok" PUT /api/settings '{"network.scan_minutes":"","network.forget_days":""}' >/dev/null
}

test_each_remote_says_which_slices_it_can_see() {
  # "the tab shows per remote which of LAN, Wi-Fi and Bluetooth it can
  # see, so 'nothing here can see Bluetooth' is a thing the tab can say."
  # The lab's VMs have neither a Wi-Fi card nor a Bluetooth radio.
  local tok body
  tok=$(token_a)
  body=$(hub_curl hub-a "$tok" POST /api/devices/scan | hub_body)
  assert_true "every scanner says whether it saw the LAN" \
    bash -c "echo '$body' | jq -e '[.scanners[] | has(\"lan\")] | all' >/dev/null"
  assert_true "every scanner says whether it could see Wi-Fi and Bluetooth (absent here, not invented)" \
    bash -c "echo '$body' | jq -e '[.scanners[] | has(\"wifi\") and has(\"bt\") and (.wifi|not) and (.bt|not)] | all' >/dev/null"
}

test_a_sighting_says_which_remote_saw_it_and_when() {
  local dev
  dev=$(hub_curl hub-a "$(token_a)" GET /api/devices | hub_body | jq -c '.devices[0] // empty')
  if [ -z "$dev" ]; then skip "no device to inspect (scan first)"; return; fi
  assert_true "a device carries its sightings: which remote, when" \
    bash -c "echo '$dev' | jq -e '.sightings | length > 0 and all(.host != \"\" and .seen != \"\")' >/dev/null"
  assert_true "a device is known by its hardware address" \
    bash -c "echo '$dev' | jq -e '.addr | test(\"^([0-9a-f]{2}:){5}[0-9a-f]{2}$\"; \"i\")' >/dev/null"
  assert_true "the guess is labelled as a guess, beside your word" \
    bash -c "echo '$dev' | jq -e 'has(\"guessName\") and has(\"guessKind\") and has(\"name\") and has(\"userKind\")' >/dev/null"
}

test_forgetting_a_device_drops_it_until_it_is_seen_again() {
  local tok id status
  tok=$(token_a)
  id=$(hub_curl hub-a "$tok" GET /api/devices | hub_body | jq -r '.devices[0].id // empty')
  if [ -z "$id" ]; then skip "no device to forget (scan first)"; return; fi
  status=$(hub_curl hub-a "$tok" DELETE "/api/devices/$id" | hub_status)
  assert_status "DELETE /api/devices/{id} forgets it" "$status" 204
  _still_listed() { hub_curl hub-a "$tok" GET /api/devices | hub_body | jq -e --argjson i "$id" '.devices[] | select(.id==$i)' >/dev/null; }
  if _still_listed; then fail "it is gone from the list"; else pass "it is gone from the list"; fi
  # The next scan brings a device that is still there back.
  hub_curl hub-a "$tok" POST /api/devices/scan >/dev/null
}
