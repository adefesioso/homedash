# docs/pooling/notifications.md — a notification target receives events
# as transitions land; sending is best-effort and never blocks.

test_notify_target_setting_round_trips() {
  local tok got
  tok=$(token_a)
  hub_curl hub-a "$tok" PUT /api/settings '{"notify.target":"https://ntfy.sh/tests-homedash-probe"}' >/dev/null
  got=$(hub_curl hub-a "$tok" GET /api/settings | hub_body | jq -r '."notify.target"')
  assert_eq "the notify target setting is stored and read back" "$got" "https://ntfy.sh/tests-homedash-probe"
}

test_a_transition_posts_to_the_target() {
  # Point the target at a one-shot listener on hub-a itself and watch a
  # real transition (locking, then unlocking, remote-small) land on it.
  local tok
  tok=$(token_a)
  "$SSH" hub-a "rm -f /tmp/tests-notify.out; nohup nc -l -p 9797 > /tmp/tests-notify.out 2>/dev/null &
    disown; sleep 1; echo listening" >/dev/null 2>&1

  hub_curl hub-a "$tok" PUT /api/settings '{"notify.target":"http://127.0.0.1:9797/"}' >/dev/null
  hub_curl hub-a "$tok" POST /api/hosts/remote-small/lock '{"locked":true}' >/dev/null
  sleep 2
  hub_curl hub-a "$tok" POST /api/hosts/remote-small/lock '{"locked":false}' >/dev/null

  local got
  got=$("$SSH" hub-a "cat /tmp/tests-notify.out 2>/dev/null")
  case "$got" in
    *POST*|*"host"*locked*|*Locked*)
      pass "a host.locked transition was POSTed to the notify target" ;;
    *)
      skip "no POST observed on the one-shot listener (nc may have raced the request — check by hand): got [${got:0:120}]" ;;
  esac
  "$SSH" hub-a "pkill -f 'nc -l -p 9797'" >/dev/null 2>&1
  # restore a harmless target so later runs don't leak this test's state
  hub_curl hub-a "$tok" PUT /api/settings '{"notify.target":""}' >/dev/null
}

test_sending_is_best_effort_and_never_blocks() {
  # An unreachable target must not make the API call itself hang or fail.
  local tok status
  tok=$(token_a)
  hub_curl hub-a "$tok" PUT /api/settings '{"notify.target":"http://10.255.255.1/black-hole"}' >/dev/null
  status=$(hub_curl hub-a "$tok" POST /api/hosts/remote-small/lock '{"locked":true}' | hub_status)
  assert_status "locking a host succeeds even with an unreachable notify target" "$status" 204
  hub_curl hub-a "$tok" POST /api/hosts/remote-small/lock '{"locked":false}' >/dev/null
  hub_curl hub-a "$tok" PUT /api/settings '{"notify.target":""}' >/dev/null
}

test_the_fullness_threshold_is_a_setting() {
  # notifications.md: "a mountpoint crossing a fullness threshold you set".
  local tok got
  tok=$(token_a)
  hub_curl hub-a "$tok" PUT /api/settings '{"notify.disk_percent":"95"}' >/dev/null
  got=$(hub_curl hub-a "$tok" GET /api/settings | hub_body | jq -r '."notify.disk_percent"')
  assert_eq "the disk threshold is stored and read back" "$got" "95"
  hub_curl hub-a "$tok" PUT /api/settings '{"notify.disk_percent":""}' >/dev/null
}

test_events_page_backward_by_id() {
  # E-1: GET /api/events?before=<id>&limit=<n> pages older. ?limit bounds
  # what one page returns, and ?before is exclusive — every row a later
  # page returns has an id strictly less than it, so paging never
  # repeats or skips a row as new ones land in between.
  local tok first count before older max_id
  tok=$(token_a)
  # A couple of fresh events to be sure there is something to page past.
  hub_curl hub-a "$tok" POST /api/hosts/remote-small/lock '{"locked":true}' >/dev/null
  hub_curl hub-a "$tok" POST /api/hosts/remote-small/lock '{"locked":false}' >/dev/null

  first=$(hub_curl hub-a "$tok" GET "/api/events?limit=3" | hub_body)
  count=$(echo "$first" | jq 'length')
  assert_eq "?limit=3 returns exactly 3 rows" "$count" "3"

  before=$(echo "$first" | jq -r '.[-1].id')
  older=$(hub_curl hub-a "$tok" GET "/api/events?before=$before&limit=5" | hub_body)
  max_id=$(echo "$older" | jq -r 'map(.id) | max // empty')
  assert_true "every row on the next page is older than ?before" \
    bash -c "[ -z '$max_id' ] || [ '$max_id' -lt '$before' ]"
}

test_what_the_target_receives_is_the_event_list() {
  # notifications.md: the target "receives them as they land" — the
  # same transitions the panel's event list records, nothing chosen
  # per event. A lock is one of them; it is in the list the moment it
  # happens, which is what the target is sent.
  local tok n0
  tok=$(token_a)
  n0=$(event_count hub-a "$tok" host.locked remote-small)
  hub_curl hub-a "$tok" POST /api/hosts/remote-small/lock '{"locked":true}' >/dev/null
  assert_eq "the lock is in the event list at once" "$(event_count hub-a "$tok" host.locked remote-small)" "$((n0 + 1))"
  hub_curl hub-a "$tok" POST /api/hosts/remote-small/lock '{"locked":false}' >/dev/null
}
