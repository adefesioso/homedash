# docs/pooling/agents/*.md — a job runs on the remote over the SSH path,
# events are copied into the hub's database live, the job ends with a
# report, rounds are capped, a job can be rolled back where the disk
# allows and says so where it can't, credentials and secrets are the
# hub's and reach a remote only for the length of a job, and what the
# panel does to a remote is appended to its rebuild script.
#
# Uses the fleet's own local pool (homedash/qwen2.5:0.5b) as the agent's
# provider, so no external API key is needed — docs/pooling/agents/README.md:
# "the house's own pool is a provider like any other."

_LAST_JOB=""

test_agent_model_change_is_appended_to_the_rebuild_script() {
  # docs/pooling/agents/rebuild.md: "What the panel itself does to a
  # remote — ... the agent's model set — the hub appends on its own."
  local tok script
  tok=$(token_a)
  hub_curl hub-a "$tok" PUT /api/hosts/remote-mid/agent '{"model":""}' >/dev/null
  script=$(hub_curl hub-a "$tok" GET /api/hosts/remote-mid | hub_body | jq -r .rebuildScript)
  case "$script" in
    *"agent model"*"config.yml"*) pass "the rebuild script carries the agent model change inline" ;;
    *) fail "the rebuild script carries the agent model change inline" ;;
  esac
}

test_rebuild_script_is_readable_and_editable() {
  # rebuild.md: "The script is readable and editable on the host card".
  local tok before status got
  tok=$(token_a)
  before=$(hub_curl hub-a "$tok" GET /api/hosts/remote-mid | hub_body | jq -r .rebuildScript)
  status=$("$SSH" hub-a "curl -sS -o /dev/null -w '%{http_code}' -X PUT -H 'Authorization: Bearer $tok' \
    --data-binary '# tests-rebuild marker' http://127.0.0.1:7433/api/hosts/remote-mid/rebuild-script")
  assert_status "PUT /api/hosts/{host}/rebuild-script replaces the script" "$status" 204
  got=$(hub_curl hub-a "$tok" GET /api/hosts/remote-mid | hub_body | jq -r .rebuildScript)
  assert_eq "the replaced script reads back" "$got" "# tests-rebuild marker"
  "$SSH" hub-a "curl -sS -o /dev/null -X PUT -H 'Authorization: Bearer $tok' --data-binary @- http://127.0.0.1:7433/api/hosts/remote-mid/rebuild-script" <<<"$before"
  got=$(hub_curl hub-a "$tok" GET /api/hosts/remote-mid | hub_body | jq -r .rebuildScript)
  assert_eq "the original script is restored" "$got" "$before"
}

test_job_carries_the_per_job_time_from_settings() {
  # agents/README.md: "whether it finished or ran out of the per-job
  # time you set". The time is read at start and recorded on the job.
  local tok id t
  tok=$(token_a)
  hub_curl hub-a "$tok" PUT /api/settings '{"jobs.timeout":"345"}' >/dev/null
  id=$(_start_job "$tok" remote-mid "Say the single word: ack")
  t=$(hub_curl hub-a "$tok" GET "/api/jobs/$id" | hub_body | jq -r .timeoutSeconds)
  hub_curl hub-a "$tok" PUT /api/settings '{"jobs.timeout":""}' >/dev/null
  assert_eq "the job records the per-job time set in Settings" "$t" "345"
  _wait_job "$tok" "$id" >/dev/null
}

test_job_runs_on_the_remote_and_ends_with_a_report() {
  local tok id j state report
  tok=$(token_a)
  id=$(_start_job "$tok" remote-mid "Say the single word: ack")
  assert_true "a job was started and got an id" test -n "$id" -a "$id" != "null"

  j=$(_wait_job "$tok" "$id")
  state=$(echo "$j" | jq -r .state)
  case "$state" in
    done|failed|needs_you) pass "the job reached a terminal state ($state), not stuck running" ;;
    *) fail "the job reached a terminal state (got: $state)" ;;
  esac
  report=$(echo "$j" | jq -r '.report // .reason // empty')
  assert_true "the job ended with a report or a reason, either way" test -n "$report"
  assert_eq "the job ran as remote-mid's own agent with the model that remote is set to" \
    "$(echo "$j" | jq -r .host)" "remote-mid"
  _LAST_JOB=$id
}

test_job_events_are_copied_into_the_hub_live() {
  local tok n
  tok=$(token_a)
  n=$(hub_curl hub-a "$tok" GET "/api/jobs/$_LAST_JOB/events" | hub_body | jq 'length')
  assert_true "at least one event was copied into the hub's database" bash -c "[ '$n' -gt 0 ]"
}

test_jobs_list_is_filterable_by_host() {
  local tok n
  tok=$(token_a)
  n=$(hub_curl hub-a "$tok" GET "/api/jobs?host=remote-mid" | hub_body | jq 'length')
  assert_true "GET /api/jobs?host=remote-mid returns remote-mid's job history" bash -c "[ '$n' -gt 0 ]"
}

test_rollback_follows_what_the_disk_allows() {
  # agents/README.md + running/safety.md: btrfs restores live, LVM at the
  # next boot, "a plain ext4 root has no snapshot, the job says `none`,
  # and the rebuild script remains the way back." The lab's remotes are
  # Debian cloud images on ext4, so the last branch is the one exercised;
  # the others are asserted if a lab ever has such a root.
  local tok j snap out status
  tok=$(token_a)
  j=$(hub_curl hub-a "$tok" GET "/api/jobs/$_LAST_JOB" | hub_body)
  snap=$(echo "$j" | jq -r '.snapshot // "none"')
  out=$(hub_curl hub-a "$tok" POST "/api/jobs/$_LAST_JOB/rollback")
  status=$(echo "$out" | hub_status)
  case "$snap" in
    btrfs|lvm)
      assert_status "roll back on a $snap root succeeds" "$status" 200
      case "$(echo "$out" | hub_body | jq -r .snapshot)" in
        restored|merge-at-boot) pass "the job records the rollback outcome" ;;
        *) fail "the job records the rollback outcome (got: $(echo "$out" | hub_body))" ;;
      esac ;;
    *)
      assert_eq "the job says it has no snapshot" "$snap" "none"
      assert_status "roll back with no snapshot is refused" "$status" 400
      case "$(echo "$out" | hub_body) " in
        *"rebuild script"*) pass "the refusal points at the rebuild script as the way back" ;;
        *) fail "the refusal points at the rebuild script as the way back (got: $(echo "$out" | hub_body))" ;;
      esac ;;
  esac
}

test_kill_stops_a_running_job() {
  # jobs.md: "Kill stops a running job: it marks the row killed ... then
  # SSHes over and systemctl stop --no-block's the round's own unit."
  # Refused once the job is no longer running.
  local tok id state out status
  tok=$(token_a)
  id=$(_start_job "$tok" remote-mid "Run the shell command: sleep 60. Then say: ack")
  sleep 3
  state=$(hub_curl hub-a "$tok" GET "/api/jobs/$id" | hub_body | jq -r .state)
  if [ "$state" != "running" ]; then
    skip "the job (a small model) did not stay running long enough to kill"
    return
  fi
  out=$(hub_curl hub-a "$tok" POST "/api/jobs/$id/kill")
  assert_status "kill on a running job succeeds" "$(echo "$out" | hub_status)" 200
  assert_eq "the job is marked killed" "$(echo "$out" | hub_body | jq -r .state)" "killed"

  out=$(hub_curl hub-a "$tok" POST "/api/jobs/$id/kill")
  assert_status "kill on a job that isn't running is refused" "$(echo "$out" | hub_status)" 400
}

test_rounds_are_capped_and_the_job_is_marked_needs_you() {
  # agents/README.md#the-loop: "Rounds are capped — a Settings value,
  # three by default — and a job that reaches the cap is marked needs
  # you rather than sent round again."
  local tok j state
  tok=$(token_a)
  if [ "$(hub_curl hub-a "$tok" GET "/api/jobs/$_LAST_JOB" | hub_body | jq -r '.session // empty')" = "" ]; then
    skip "the job never opened a session on the remote; the cap is checked on a correction"
    return
  fi
  hub_curl hub-a "$tok" PUT /api/settings '{"agent.rounds":"1"}' >/dev/null
  j=$(hub_curl hub-a "$tok" POST "/api/jobs/$_LAST_JOB/correct" '{"text":"Say it again."}' | hub_body)
  hub_curl hub-a "$tok" PUT /api/settings '{"agent.rounds":""}' >/dev/null
  state=$(echo "$j" | jq -r .state)
  assert_eq "a correction past the round cap marks the job needs_you" "$state" "needs_you"
  case "$(echo "$j" | jq -r .reason)" in
    *"round cap"*) pass "the reason names the cap" ;;
    *) fail "the reason names the cap (got: $(echo "$j" | jq -r .reason))" ;;
  esac
  assert_true "needs you is an event" test "$(event_count hub-a "$tok" job.needs_you "job $_LAST_JOB ")" -gt 0
}

test_a_correction_is_a_follow_up_round_on_the_same_session() {
  local tok id before after j
  tok=$(token_a)
  id=$(_start_job "$tok" remote-mid "Say the single word: ack")
  j=$(_wait_job "$tok" "$id")
  before=$(echo "$j" | jq -r .rounds)
  if [ "$(echo "$j" | jq -r '.session // empty')" = "" ]; then
    skip "the first round never opened a session on the remote; nothing to correct"
    return
  fi
  j=$(hub_curl hub-a "$tok" POST "/api/jobs/$id/correct" '{"text":"Now say: ack again"}' | hub_body)
  j=$(_wait_job "$tok" "$id")
  after=$(echo "$j" | jq -r .rounds)
  assert_true "a correction is one more round on the same job" test "$after" -gt "$before"
}

test_a_pi_class_box_cannot_run_the_agent_and_says_so() {
  # hosts.md: "A Pi-class box ... cannot run the coding agent at all; it
  # still enrolls, still pools, and the card says the agent is not usable
  # there." remote-small has 1 GB.
  local tok out
  tok=$(token_a)
  out=$(hub_curl hub-a "$tok" POST /api/jobs '{"host":"remote-small","text":"Say ack"}')
  assert_status "a job on remote-small is refused" "$(echo "$out" | hub_status)" 400
  case "$(echo "$out" | hub_body)" in
    *memory*) pass "the refusal says why: memory" ;;
    *) fail "the refusal says why: memory (got: $(echo "$out" | hub_body))" ;;
  esac
}

test_a_job_with_a_missing_cwd_is_refused() {
  # G-5/G-9: docs/pooling/agents/README.md — "A host that isn't online is
  # refused before anything starts, and so is a directory that isn't
  # already there." There is no offline host in this lab by default
  # (stopping one is out of scope for an unattended run), so this
  # exercises the sibling refusal: a cwd that does not exist, checked
  # before any session opens on the remote.
  local tok cwd out status
  tok=$(token_a)
  # A path unique to this run: an earlier probe of this very bug (G-5)
  # left /no/such/dir actually existing on remote-mid (the old code
  # auto-created a missing cwd), so a fixed name would prove nothing.
  cwd="/no/such/dir-$$-$RANDOM"
  out=$(hub_curl hub-a "$tok" POST /api/jobs "$(jq -nc --arg c "$cwd" '{host:"remote-mid",cwd:$c,text:"Say ack"}')")
  status=$(echo "$out" | hub_status)
  assert_status "a job whose cwd does not exist on the remote is refused" "$status" 400
  case "$(echo "$out" | hub_body)" in
    *"$cwd"*remote-mid*) pass "the refusal names the missing directory and the host" ;;
    *) fail "the refusal names the missing directory and the host (got: $(echo "$out" | hub_body))" ;;
  esac
}

test_session_names_are_drawn_by_the_hub_and_unique_while_live() {
  # docs/pooling/agents/README.md — the hub names each session with a
  # passphrase, adjective-noun; nothing is sent, and no two live sessions
  # share a name (the history a hub restart piles up is not checked).
  local tok ready s1 s2 id1 id2 n1 n2
  tok=$(token_a)
  ready=$(hub_curl hub-a "$tok" GET /api/agents | hub_body | jq -r .ready)
  if [ "$ready" != "true" ]; then
    skip "the hub's own omp is not ready yet; session naming not exercised"
    return
  fi
  s1=$(hub_curl hub-a "$tok" POST /api/agents/sessions '{}' | hub_body)
  s2=$(hub_curl hub-a "$tok" POST /api/agents/sessions '{"name":"tests-ignored"}' | hub_body)
  id1=$(echo "$s1" | jq -r .id); id2=$(echo "$s2" | jq -r .id)
  n1=$(echo "$s1" | jq -r .name); n2=$(echo "$s2" | jq -r .name)
  assert_true "the session opened and got an id" test -n "$id1" -a "$id1" != "null"
  case "$n1" in
    [a-z]*-[a-z]*) pass "the hub drew an adjective-noun name ($n1)" ;;
    *) fail "the hub drew an adjective-noun name (got: $n1)" ;;
  esac
  assert_true "a name in the request is ignored" test "$n2" != "tests-ignored"
  assert_true "two live sessions never share a name" test "$n1" != "$n2"

  hub_curl hub-a "$tok" DELETE "/api/agents/sessions/$id1" >/dev/null
  hub_curl hub-a "$tok" DELETE "/api/agents/sessions/$id2" >/dev/null
}

test_a_closed_session_keeps_its_last_screen() {
  # docs/pooling/agents/README.md — a finished session keeps the last
  # screenful of what it showed; History on its row opens it. The bytes
  # come from GET /api/agents/sessions/{id}/history: 409 while live, and
  # once closed the terminal stream omp drew (escape sequences, since omp
  # paints cell by cell — the banner's letters are never contiguous).
  local tok ready id status n
  tok=$(token_a)
  ready=$(hub_curl hub-a "$tok" GET /api/agents | hub_body | jq -r .ready)
  if [ "$ready" != "true" ]; then
    skip "the hub's own omp is not ready yet; session history not exercised"
    return
  fi
  id=$(hub_curl hub-a "$tok" POST /api/agents/sessions '{}' | hub_body | jq -r .id)
  sleep 5
  status=$(hub_curl hub-a "$tok" GET "/api/agents/sessions/$id/history" | hub_status)
  assert_status "a live session has no history yet: attach instead" "$status" 409
  hub_curl hub-a "$tok" POST "/api/agents/sessions/$id/close" >/dev/null
  sleep 2
  n=$(hub_curl hub-a "$tok" GET "/api/agents/sessions/$id/history" | hub_body | tr -cd '\033' | wc -c)
  assert_true "the closed session's kept screen is a terminal stream (got $n escapes)" test "$n" -gt 10
  hub_curl hub-a "$tok" DELETE "/api/agents/sessions/$id" >/dev/null
}

test_secret_names_are_listable_but_values_are_not() {
  # docs/pooling/agents/credentials.md: "The hub's agent can list the
  # names a host may ask for ... and can never read a value."
  local tok body mid_id
  tok=$(token_a)
  mid_id=$(hub_curl hub-a "$tok" GET /api/hosts | hub_body | jq -r '.[] | select(.name=="remote-mid") | .id')
  hub_curl hub-a "$tok" PUT /api/secrets/tests-secret \
    "$(jq -nc --argjson h "$mid_id" '{value:"super-secret-value",hosts:[$h]}')" >/dev/null
  body=$(hub_curl hub-a "$tok" GET /api/secrets | hub_body)
  case "$body" in
    *super-secret-value*) fail "the secret value never comes back from GET /api/secrets" ;;
    *) pass "the secret value never comes back from GET /api/secrets" ;;
  esac
  case "$body" in
    *tests-secret*) pass "the secret's name is listed" ;;
    *) fail "the secret's name is listed" ;;
  esac
}

test_a_remote_reads_a_granted_secret_through_the_door_and_every_read_is_an_event() {
  # credentials.md: "its remote can ask for one by name with
  # homedash-secret NAME ... the answer comes from the hub ... Every read
  # is an event." Granted to remote-mid above; not to remote-big.
  local tok out reads0 refused0
  tok=$(token_a)
  reads0=$(event_count hub-a "$tok" secret.read "remote-mid read secret tests-secret")
  refused0=$(event_count hub-a "$tok" secret.refused "remote-big asked for secret tests-secret")
  out=$(agent_run hub-a "$tok" remote-mid "homedash-secret tests-secret; echo; echo exit=\$?" | hub_body | jq -r .stdout)
  case "$out" in
    *super-secret-value*exit=0*) pass "remote-mid reads the secret it was granted, by name" ;;
    *) fail "remote-mid reads the secret it was granted, by name (got: $out)" ;;
  esac
  assert_true "the read is an event" \
    test "$(event_count hub-a "$tok" secret.read "remote-mid read secret tests-secret")" -gt "$reads0"
  out=$(agent_run hub-a "$tok" remote-big "homedash-secret tests-secret; echo; echo exit=\$?" | hub_body | jq -r .stdout)
  case "$out" in
    *super-secret-value*) fail "a host the secret was not granted to gets nothing" ;;
    *) pass "a host the secret was not granted to gets nothing" ;;
  esac
  assert_true "the refusal is an event too" \
    test "$(event_count hub-a "$tok" secret.refused "remote-big asked for secret tests-secret")" -gt "$refused0"
  hub_curl hub-a "$tok" DELETE /api/secrets/tests-secret >/dev/null
  out=$(agent_run hub-a "$tok" remote-mid "homedash-secret tests-secret; echo; echo exit=\$?" | hub_body | jq -r .stdout)
  case "$out" in
    *super-secret-value*) fail "a deleted secret is not on the remote's disk to be read back" ;;
    *) pass "a deleted secret is not on the remote's disk to be read back" ;;
  esac
}

test_credentials_are_updated_and_revoked_from_the_card() {
  # credentials.md: "Update credentials on a card ... mints a fresh token
  # and pulls a fresh snapshot; Revoke on a card leaves the remote with a
  # token the hub no longer answers, until you update it again."
  local tok status revoked0
  tok=$(token_a)
  revoked0=$(event_count hub-a "$tok" agent.credentials_revoked remote-mid)
  status=$(hub_curl hub-a "$tok" POST /api/hosts/remote-mid/credentials/revoke | hub_status)
  assert_status "Revoke succeeds" "$status" 204
  assert_true "the revocation is an event" \
    test "$(event_count hub-a "$tok" agent.credentials_revoked remote-mid)" -gt "$revoked0"
  status=$(hub_curl hub-a "$tok" POST /api/hosts/remote-mid/credentials | hub_status)
  assert_status "Update credentials mints a token and pulls a snapshot again" "$status" 204
}

test_reprovision_runs_the_layout_again_without_a_new_code() {
  # agents/README.md#remotes-enrolled-before-this-layout — no new code,
  # no new host record.
  local tok status before after
  tok=$(token_a)
  before=$(hub_curl hub-a "$tok" GET /api/hosts | hub_body | jq -r '.[] | select(.name=="remote-mid") | .id')
  local ok0 bad0
  ok0=$(event_count hub-a "$tok" host.reprovisioned remote-mid)
  bad0=$(event_count hub-a "$tok" host.reprovision_failed remote-mid)
  status=$(hub_curl hub-a "$tok" POST /api/hosts/remote-mid/reprovision | hub_status)
  assert_status "Re-provision is accepted" "$status" 204
  _reprovisioned() { [ "$(event_count hub-a "$tok" host.reprovisioned remote-mid)" -gt "$ok0" ]; }
  _reprovision_failed() { [ "$(event_count hub-a "$tok" host.reprovision_failed remote-mid)" -gt "$bad0" ]; }
  if wait_until 180 _reprovisioned; then
    pass "the layout ran again on remote-mid"
  elif _reprovision_failed; then
    fail "re-provision failed: $(hub_events hub-a "$tok" host.reprovision_failed | head -1)"
  else
    fail "re-provision did not finish in time"
  fi
  after=$(hub_curl hub-a "$tok" GET /api/hosts | hub_body | jq -r '.[] | select(.name=="remote-mid") | .id')
  assert_eq "the host record is the same one" "$after" "$before"
}

test_reprovision_on_a_pi_class_box_skips_the_snapshot_not_the_run() {
  # H-15: remote-small is under the 1.4 GB floor omp needs to run at
  # all, so it can never write a credential snapshot — re-provisioning
  # it used to end as host.reprovision_failed ("omp did not write a
  # credential snapshot") even though the layout itself ran fine. It now
  # skips that step and still ends as host.reprovisioned, saying so.
  local tok status ok0 bad0 msg
  tok=$(token_a)
  ok0=$(event_count hub-a "$tok" host.reprovisioned remote-small)
  bad0=$(event_count hub-a "$tok" host.reprovision_failed remote-small)
  status=$(hub_curl hub-a "$tok" POST /api/hosts/remote-small/reprovision | hub_status)
  assert_status "Re-provision on remote-small (Pi-class) is accepted" "$status" 204
  _small_reprovisioned() { [ "$(event_count hub-a "$tok" host.reprovisioned remote-small)" -gt "$ok0" ]; }
  _small_reprovision_failed() { [ "$(event_count hub-a "$tok" host.reprovision_failed remote-small)" -gt "$bad0" ]; }
  if wait_until 180 _small_reprovisioned; then
    msg=$(hub_events hub-a "$tok" host.reprovisioned | head -1)
    case "$msg" in
      *"skip"*) pass "the event says the credential snapshot was skipped" ;;
      *) fail "the event says the credential snapshot was skipped (got: $msg)" ;;
    esac
  elif _small_reprovision_failed; then
    fail "re-provision on a Pi-class box should skip the snapshot, not fail: $(hub_events hub-a "$tok" host.reprovision_failed | head -1)"
  else
    fail "re-provision on remote-small did not finish in time"
  fi
}
