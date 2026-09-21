# docs/pooling/tasks.md — a task is a command, a host, and a schedule;
# there is no crontab anywhere, and Run now lets you test a schedule.

_task_cleanup() {
  local tok=$1 id=$2
  [ -n "$id" ] && [ "$id" != "null" ] && hub_curl hub-a "$tok" DELETE "/api/tasks/$id" >/dev/null
}

# _wait_finished_run <tok> <task-id> -> prints the runs array once at
# least one row has a non-null exitCode (rows are inserted before the
# command finishes, so "length > 0" alone can catch one still running).
_wait_finished_run() {
  local tok=$1 id=$2 i=0 runs=""
  while [ $i -lt 20 ]; do
    runs=$(hub_curl hub-a "$tok" GET "/api/tasks/$id/runs" | hub_body)
    if echo "$runs" | jq -e '[.[] | select(.exitCode != null)] | length > 0' >/dev/null 2>&1; then
      echo "$runs"; return
    fi
    sleep 3; i=$((i+1))
  done
  echo "$runs"
}

_host_id() { hub_curl hub-a "$(token_a)" GET /api/hosts | hub_body | jq -r --arg n "$1" '.[] | select(.name==$n) | .id'; }

test_create_run_now_and_see_it_recorded() {
  local tok mid_id body id status runs exit_code
  tok=$(token_a)
  mid_id=$(_host_id remote-mid)
  body=$(hub_curl hub-a "$tok" POST /api/tasks \
    "$(jq -nc --argjson h "$mid_id" '{name:"tests-echo",hostId:$h,command:"echo task-ran-ok",schedule:"@yearly",timeoutSeconds:30,enabled:true}')" \
    | hub_body)
  id=$(echo "$body" | jq -r .id)
  assert_true "a task was created and got an id" test -n "$id" -a "$id" != "null"

  status=$(hub_curl hub-a "$tok" POST "/api/tasks/$id/run" | hub_status)
  assert_status "Run now accepts the task" "$status" 204

  runs=$(_wait_finished_run "$tok" "$id")
  assert_true "the run shows up in /api/tasks/{id}/runs" bash -c "[ \"\$(echo '$runs' | jq 'length')\" -gt 0 ]"
  exit_code=$(echo "$runs" | jq -r '[.[] | select(.exitCode != null)][0].exitCode')
  assert_eq "the run's exit code was recorded as 0" "$exit_code" "0"
  local output
  output=$(echo "$runs" | jq -r '[.[] | select(.exitCode != null)][0].output')
  case "$output" in
    *task-ran-ok*) pass "the recorded output is what actually ran on remote-mid" ;;
    *) fail "the recorded output is what actually ran on remote-mid (got: $output)" ;;
  esac

  _task_cleanup "$tok" "$id"
}

test_task_command_goes_through_the_gate() {
  local tok mid_id body id runs out
  tok=$(token_a)
  mid_id=$(_host_id remote-mid)
  body=$(hub_curl hub-a "$tok" POST /api/tasks \
    "$(jq -nc --argjson h "$mid_id" '{name:"tests-refused",hostId:$h,command:"reboot",schedule:"@yearly",timeoutSeconds:30,enabled:true}')" \
    | hub_body)
  id=$(echo "$body" | jq -r .id 2>/dev/null)
  # docs/pooling/tasks.md: "goes through the same gate ... on save and on
  # every fire, so a command that becomes refusable stops running". The
  # task should either be refused at save, or refused on every fire.
  if [ -z "$id" ] || [ "$id" = "null" ]; then
    pass "a task whose command the gate refuses is refused at save"
    return
  fi
  hub_curl hub-a "$tok" POST "/api/tasks/$id/run" >/dev/null
  runs=$(_wait_finished_run "$tok" "$id")
  out=$(echo "$runs" | jq -r '[.[] | select(.exitCode != null)][0] | (.output + " exit=" + (.exitCode|tostring))' 2>/dev/null)
  case "$out" in
    *efused*|*firewall*|*"exit=1"*|*"exit=2"*) pass "the fire of a gate-refusable task did not reboot the host (got: $out)" ;;
    *) fail "the fire of a gate-refusable task did not reboot the host (got: $out)" ;;
  esac
  _task_cleanup "$tok" "$id"
}

test_fleet_wide_task_names_every_remote() {
  # hostId:0 = every enrolled remote (docs: "A task names one remote, or
  # every enrolled remote").
  local tok body id host_field status
  tok=$(token_a)
  body=$(hub_curl hub-a "$tok" POST /api/tasks \
    '{"name":"tests-fleet-wide","hostId":0,"command":"echo hi","schedule":"@yearly","timeoutSeconds":30,"enabled":true}' \
    | hub_body)
  id=$(echo "$body" | jq -r .id)
  host_field=$(echo "$body" | jq -r '.hostId // .host_id // empty')
  assert_eq "a fleet-wide task has hostId 0" "$host_field" "0"

  status=$(hub_curl hub-a "$tok" POST "/api/tasks/$id/run" | hub_status)
  assert_status "Run now accepts a fleet-wide task" "$status" 204
  local runs n
  runs=$(_wait_finished_run "$tok" "$id")
  sleep 3  # let the rest of the (sequential) fan-out land
  runs=$(hub_curl hub-a "$tok" GET "/api/tasks/$id/runs" | hub_body)
  n=$(echo "$runs" | jq -r '[.[] | .host] | unique | length')
  assert_true "the fan-out ran on more than one host" test "$n" -gt 1

  _task_cleanup "$tok" "$id"
}

test_a_hanging_command_is_ended_by_the_per_task_timeout() {
  # docs/pooling/tasks.md: "a hanging command is ended by a per-task
  # timeout" — and the fire is recorded like any other, with a non-zero
  # exit code rather than a row left open.
  local tok mid_id id runs code t0 t1
  tok=$(token_a)
  mid_id=$(_host_id remote-mid)
  id=$(hub_curl hub-a "$tok" POST /api/tasks \
    "$(jq -nc --argjson h "$mid_id" '{name:"tests-hang",hostId:$h,command:"sleep 120",schedule:"@yearly",timeoutSeconds:5,enabled:true}')" \
    | hub_body | jq -r .id)
  t0=$(date +%s)
  hub_curl hub-a "$tok" POST "/api/tasks/$id/run" >/dev/null
  runs=$(_wait_finished_run "$tok" "$id")
  t1=$(date +%s)
  code=$(echo "$runs" | jq -r '[.[] | select(.exitCode != null)][0].exitCode // empty')
  assert_true "the run was ended and recorded" test -n "$code"
  assert_ne "the timed-out run's exit code is not 0" "$code" "0"
  assert_true "it ended well before the command's own 120 s" test $((t1 - t0)) -lt 60
  _task_cleanup "$tok" "$id"
}

test_a_task_that_starts_failing_and_one_that_recovers_are_transitions() {
  # docs/pooling/tasks.md: "A task that starts failing, and one that
  # recovers, reaches the notification target." A transition, not a
  # state: the second failure in a row is not a second event.
  local tok mid_id id failing0 ok0
  tok=$(token_a)
  mid_id=$(_host_id remote-mid)
  failing0=$(event_count hub-a "$tok" task.failing tests-flaky)
  ok0=$(event_count hub-a "$tok" task.ok tests-flaky)
  id=$(hub_curl hub-a "$tok" POST /api/tasks \
    "$(jq -nc --argjson h "$mid_id" '{name:"tests-flaky",hostId:$h,command:"false",schedule:"@yearly",timeoutSeconds:30,enabled:true}')" \
    | hub_body | jq -r .id)
  hub_curl hub-a "$tok" POST "/api/tasks/$id/run" >/dev/null
  _wait_finished_run "$tok" "$id" >/dev/null
  assert_eq "the first failure is one task.failing event" \
    "$(event_count hub-a "$tok" task.failing tests-flaky)" "$((failing0 + 1))"
  hub_curl hub-a "$tok" POST "/api/tasks/$id/run" >/dev/null
  sleep 5
  assert_eq "failing again is not another event" \
    "$(event_count hub-a "$tok" task.failing tests-flaky)" "$((failing0 + 1))"

  hub_curl hub-a "$tok" PUT "/api/tasks/$id" \
    "$(jq -nc --argjson h "$mid_id" '{name:"tests-flaky",hostId:$h,command:"true",schedule:"@yearly",timeoutSeconds:30,enabled:true}')" >/dev/null
  hub_curl hub-a "$tok" POST "/api/tasks/$id/run" >/dev/null
  sleep 5
  assert_eq "recovering is one task.ok event" \
    "$(event_count hub-a "$tok" task.ok tests-flaky)" "$((ok0 + 1))"
  _task_cleanup "$tok" "$id"
}

test_run_now_on_a_disabled_task_is_refused() {
  # T-9: docs/pooling/tasks.md — "not on a disabled task, which Run now
  # refuses, the same as the clock would."
  local tok mid_id id status
  tok=$(token_a)
  mid_id=$(_host_id remote-mid)
  id=$(hub_curl hub-a "$tok" POST /api/tasks \
    "$(jq -nc --argjson h "$mid_id" '{name:"tests-disabled",hostId:$h,command:"echo hi",schedule:"@yearly",timeoutSeconds:30,enabled:false}')" \
    | hub_body | jq -r .id)
  status=$(hub_curl hub-a "$tok" POST "/api/tasks/$id/run" | hub_status)
  assert_status "Run now on a disabled task is refused" "$status" 409
  _task_cleanup "$tok" "$id"
}

test_task_timeout_is_bounded() {
  # T-1: docs/pooling/tasks.md — "from 1 to 86400 seconds (a day) —
  # blank keeps the default of 600."
  local tok mid_id status
  tok=$(token_a)
  mid_id=$(_host_id remote-mid)
  status=$(hub_curl hub-a "$tok" POST /api/tasks \
    "$(jq -nc --argjson h "$mid_id" '{name:"tests-timeout-0",hostId:$h,command:"echo hi",schedule:"@yearly",timeoutSeconds:0,enabled:true}')" \
    | hub_status)
  assert_status "a timeout of 0 is refused" "$status" 400
  status=$(hub_curl hub-a "$tok" POST /api/tasks \
    "$(jq -nc --argjson h "$mid_id" '{name:"tests-timeout-big",hostId:$h,command:"echo hi",schedule:"@yearly",timeoutSeconds:100000,enabled:true}')" \
    | hub_status)
  assert_status "a timeout over 86400 is refused" "$status" 400
  local body id got
  body=$(hub_curl hub-a "$tok" POST /api/tasks \
    "$(jq -nc --argjson h "$mid_id" '{name:"tests-timeout-blank",hostId:$h,command:"echo hi",schedule:"@yearly",enabled:true}')" \
    | hub_body)
  id=$(echo "$body" | jq -r .id)
  got=$(echo "$body" | jq -r .timeoutSeconds)
  assert_eq "a blank timeout defaults to 600" "$got" "600"
  _task_cleanup "$tok" "$id"
}

test_the_tasks_tab_is_the_list_of_everything_scheduled() {
  # "The Tasks tab is the list of everything scheduled in the house" —
  # and a task edited from the panel is the task that fires: no crontab
  # to fall out of step.
  local tok mid_id id names out
  tok=$(token_a)
  mid_id=$(_host_id remote-mid)
  id=$(hub_curl hub-a "$tok" POST /api/tasks \
    "$(jq -nc --argjson h "$mid_id" '{name:"tests-listed",hostId:$h,command:"echo one",schedule:"@yearly",timeoutSeconds:30,enabled:true}')" \
    | hub_body | jq -r .id)
  names=$(hub_curl hub-a "$tok" GET /api/tasks | hub_body | jq -r '.[].name' | tr '\n' ' ')
  case " $names " in
    *" tests-listed "*) pass "GET /api/tasks lists the task" ;;
    *) fail "GET /api/tasks lists the task (got: $names)" ;;
  esac
  out=$("$SSH" remote-mid "sudo cat /etc/crontab /etc/cron.d/* 2>/dev/null; sudo crontab -l -u homedash 2>/dev/null; crontab -l 2>/dev/null; echo done")
  case "$out" in
    *tests-listed*|*"echo one"*) fail "there is no crontab anywhere carrying the task" ;;
    *) pass "there is no crontab anywhere carrying the task" ;;
  esac
  _task_cleanup "$tok" "$id"
}
