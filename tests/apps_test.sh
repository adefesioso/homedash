# docs/pooling/apps.md — catalog, placement arithmetic, and "it gives the
# same answer twice."

test_placement_refuses_what_structurally_cannot_fit() {
  # remote-small is 1 core; nothing about margin or disk trend can make a
  # 2-core request fit it.
  local tok body
  tok=$(token_a)
  body=$(hub_curl hub-a "$tok" POST /api/apps/placement \
    '{"cores":2,"memoryMB":256,"diskGB":1}' | hub_body)
  local small_fits why
  small_fits=$(echo "$body" | jq -r '.[] | select(.host=="remote-small") | .fits')
  assert_eq "remote-small (1 core) does not fit a 2-core app" "$small_fits" "false"
  why=$(echo "$body" | jq -r '.[] | select(.host=="remote-small") | .why')
  assert_true "the verdict carries a reason sentence" test -n "$why"
}

test_placement_leaves_margin_not_just_bare_fit() {
  # docs/pooling/apps.md: "a candidate has to leave margin, not just fit
  # exactly." Asking for essentially all of a host's free memory must not
  # verdict fits:true.
  local tok body fits
  tok=$(token_a)
  body=$(hub_curl hub-a "$tok" POST /api/apps/placement \
    "$(jq -nc '{cores:1,memoryMB:990,diskGB:1}')" | hub_body)
  fits=$(echo "$body" | jq -r '.[] | select(.host=="remote-small") | .fits')
  assert_eq "asking for ~all of remote-small's 1GB does not fit (needs margin)" "$fits" "false"
}

test_placement_is_deterministic() {
  # "it gives the same answer twice" — no inference involved.
  local tok a b
  tok=$(token_a)
  a=$(hub_curl hub-a "$tok" POST /api/apps/placement '{"cores":1,"memoryMB":512,"diskGB":1}' | hub_body | jq -S .)
  b=$(hub_curl hub-a "$tok" POST /api/apps/placement '{"cores":1,"memoryMB":512,"diskGB":1}' | hub_body | jq -S .)
  assert_eq "the same needs produce the same ranking twice" "$a" "$b"
}

test_apps_tab_reads_the_remote_live() {
  # docs/pooling/apps.md: "nothing about an installed app is stored on the
  # hub" — /api/apps asks the remotes on every call.
  local tok status
  tok=$(token_a)
  status=$(hub_curl hub-a "$tok" GET /api/apps | hub_status)
  assert_status "GET /api/apps (a live fleet read) succeeds" "$status" 200
}

# docs/pooling/apps.md: "You can install or update a stack, read its
# compose file and recent logs, start, stop, restart or pull it, and
# remove it with or without its volumes." One stack, the whole life.
# traefik/whoami is the image the lab's existing whoami stack already
# holds on remote-mid, so no pull is needed.
_STACK=tests-stack
_COMPOSE='services:
  web:
    image: traefik/whoami
    ports:
      - "18080:80"
'
_stack_row() { hub_curl hub-a "$(token_a)" GET /api/apps | hub_body | jq -c --arg n "$_STACK" '.stacks[] | select(.name==$n and .host=="remote-mid")'; }
_stack_status() { _stack_row | jq -r '.status // empty'; }
_stack_cleanup() { hub_curl hub-a "$(token_a)" POST "/api/hosts/remote-mid/apps/$_STACK" '{"action":"remove","volumes":true}' >/dev/null 2>&1; }

test_a_stack_is_deployed_from_a_pasted_compose_file() {
  local tok out status
  tok=$(token_a)
  _stack_cleanup
  out=$(hub_curl hub-a "$tok" POST /api/hosts/remote-mid/apps \
    "$(jq -nc --arg n "$_STACK" --arg c "$_COMPOSE" '{name:$n,compose:$c,env:""}')")
  status=$(echo "$out" | hub_status)
  assert_status "deploying the stack on remote-mid succeeds" "$status" 200
  _running() { case "$(_stack_status)" in *running*) return 0 ;; *) return 1 ;; esac; }
  assert_true "the Apps tab lists it, live from the remote, as running" wait_until 40 _running
  assert_true "it is a managed stack: the hub can read and update its file" test "$(_stack_row | jq -r .managed)" = "true"
  assert_true "the stack answers on its port, on the remote" \
    bash -c "$(printf '%q' "$SSH") remote-mid 'curl -sS -m 5 http://127.0.0.1:18080/' 2>/dev/null | grep -q Hostname"
  assert_true "deploying is an event" test "$(event_count hub-a "$tok" app.deployed "$_STACK")" -gt 0
}

test_the_compose_file_and_logs_are_readable() {
  local tok body status
  tok=$(token_a)
  body=$(hub_curl hub-a "$tok" GET "/api/hosts/remote-mid/apps/$_STACK/file" | hub_body)
  case "$(echo "$body" | jq -r .compose)" in
    *"traefik/whoami"*) pass "the compose file reads back from the remote" ;;
    *) fail "the compose file reads back from the remote (got: $body)" ;;
  esac
  status=$(hub_curl hub-a "$tok" GET "/api/hosts/remote-mid/apps/$_STACK/logs?lines=20" | hub_status)
  assert_status "recent logs are readable" "$status" 200
}

test_stop_start_and_restart_act_on_the_stack() {
  local tok status
  tok=$(token_a)
  status=$(hub_curl hub-a "$tok" POST "/api/hosts/remote-mid/apps/$_STACK" '{"action":"stop"}' | hub_status)
  assert_status "stop is accepted" "$status" 200
  _stopped() { case "$(_stack_status)" in *running*) return 1 ;; *) return 0 ;; esac; }
  assert_true "the stack no longer reads running" wait_until 30 _stopped
  status=$(hub_curl hub-a "$tok" POST "/api/hosts/remote-mid/apps/$_STACK" '{"action":"start"}' | hub_status)
  assert_status "start is accepted" "$status" 200
  _running() { case "$(_stack_status)" in *running*) return 0 ;; *) return 1 ;; esac; }
  assert_true "the stack reads running again" wait_until 30 _running
  status=$(hub_curl hub-a "$tok" POST "/api/hosts/remote-mid/apps/$_STACK" '{"action":"restart"}' | hub_status)
  assert_status "restart is accepted" "$status" 200
}

test_pull_action_keeps_the_stack_running() {
  # A-3: pull then up runs through the same compose() wrapper twice, not
  # a shell "&&" that drops the "-f" the second half needs. The image is
  # already local, so this is quick.
  local tok status
  tok=$(token_a)
  status=$(hub_curl hub-a "$tok" POST "/api/hosts/remote-mid/apps/$_STACK" '{"action":"pull"}' | hub_status)
  assert_status "pull is accepted" "$status" 200
  _running() { case "$(_stack_status)" in *running*) return 0 ;; *) return 1 ;; esac; }
  assert_true "the stack is still running after pull" wait_until 30 _running
}

test_remove_with_volumes_leaves_nothing_behind() {
  local tok status
  tok=$(token_a)
  status=$(hub_curl hub-a "$tok" POST "/api/hosts/remote-mid/apps/$_STACK" '{"action":"remove","volumes":true}' | hub_status)
  assert_status "remove with volumes is accepted" "$status" 200
  _gone() { [ -z "$(_stack_row)" ]; }
  assert_true "the stack is gone from the live list" wait_until 30 _gone
  assert_true "removing is an event" test "$(event_count hub-a "$tok" app.removed "$_STACK")" -gt 0
}

# apps.md: "a failed deploy is removed" — remote-mid's port 8000 is
# already bound by the lab's persistent whoami stack (memory: lab-state),
# so deploying onto it is guaranteed to fail docker's own port bind.
_PORTCLASH=tests-portclash
_PORTCLASH_COMPOSE='services:
  web:
    image: traefik/whoami
    ports:
      - "8000:80"
'
test_deploy_binding_a_taken_port_fails_and_cleans_up() {
  local tok out status
  tok=$(token_a)
  hub_curl hub-a "$tok" POST "/api/hosts/remote-mid/apps/$_PORTCLASH" '{"action":"remove","volumes":true}' >/dev/null 2>&1
  out=$(hub_curl hub-a "$tok" POST /api/hosts/remote-mid/apps \
    "$(jq -nc --arg n "$_PORTCLASH" --arg c "$_PORTCLASH_COMPOSE" '{name:$n,compose:$c,env:""}')")
  status=$(echo "$out" | hub_status)
  assert_true "deploying onto a taken port fails" test "$status" != "200"
  _clash_row() { hub_curl hub-a "$tok" GET /api/apps | hub_body | jq -c --arg n "$_PORTCLASH" '.stacks[] | select(.name==$n and .host=="remote-mid")'; }
  _gone() { [ -z "$(_clash_row)" ]; }
  assert_true "the failed deploy leaves no stack behind" wait_until 20 _gone
  assert_true "the lab's own whoami stack on remote-mid is unaffected" \
    bash -c "$(printf '%q' "$SSH") remote-mid 'curl -sS -m 5 http://127.0.0.1:8000/' 2>/dev/null | grep -q Hostname"
}

# docs/running/safety.md / docs/pooling/apps.md: the compose gate refuses
# an agent, and warns (but does not stop) the panel/CLI door.
_PRIV=tests-priv
_PRIV_COMPOSE='services:
  web:
    image: traefik/whoami
    privileged: true
'
test_privileged_compose_deploys_with_a_warning_for_an_admin() {
  # The agent-refusal half of this same gate is TestComposeRefusal in
  # hub/internal/gate/gate_test.go: minting a job-token/window/MCP-agent
  # credential from this shell script isn't practical in this lab, so
  # that half is a Go unit test instead (per the brief).
  local tok out status warning
  tok=$(token_a)
  hub_curl hub-a "$tok" POST "/api/hosts/remote-mid/apps/$_PRIV" '{"action":"remove","volumes":true}' >/dev/null 2>&1
  out=$(hub_curl hub-a "$tok" POST /api/hosts/remote-mid/apps \
    "$(jq -nc --arg n "$_PRIV" --arg c "$_PRIV_COMPOSE" '{name:$n,compose:$c,env:""}')")
  status=$(echo "$out" | hub_status)
  assert_status "an admin's privileged compose still deploys" "$status" 200
  warning=$(echo "$out" | hub_body | jq -r '.warning // empty')
  assert_true "the response carries a warning naming what an agent would be refused" test -n "$warning"
  hub_curl hub-a "$tok" POST "/api/hosts/remote-mid/apps/$_PRIV" '{"action":"remove","volumes":true}' >/dev/null 2>&1
}

test_a_catalog_entry_you_add_can_be_installed_and_removed() {
  # apps.md: "save a compose file once as a catalog entry ... and install
  # it again later with one click."
  local tok status names
  tok=$(token_a)
  status=$(hub_curl hub-a "$tok" POST /api/apps/catalog \
    "$(jq -nc --arg c "$_COMPOSE" '{name:"tests-entry",title:"Tests entry",description:"a test",needs:{cores:1,memoryMB:64,diskGB:1},volumes:[],compose:$c}')" | hub_status)
  assert_status "a catalog entry is accepted" "$status" 204
  names=$(hub_curl hub-a "$tok" GET /api/apps/catalog | hub_body | jq -r '.[] | select(.name=="tests-entry") | .name')
  assert_eq "it is listed in the catalog" "$names" "tests-entry"
  status=$(hub_curl hub-a "$tok" DELETE /api/apps/catalog/tests-entry | hub_status)
  assert_status "it can be removed again" "$status" 204
}

test_deleting_a_catalog_entry_that_was_never_added_is_refused() {
  local tok status
  tok=$(token_a)
  status=$(hub_curl hub-a "$tok" DELETE /api/apps/catalog/no-such-entry | hub_status)
  assert_status "deleting a name nothing was ever added under is refused" "$status" 404
}
