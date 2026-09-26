# docs/running/safety.md — the gate. Every refusal below is exercised
# through POST /api/hosts/{host}/run exactly as the panel, a window or an
# outside assistant would reach it: "the gate is in code, at the API."

_gate_run() { # _gate_run <host> <command>
  local tok=$1 host=$2 cmd=$3
  hub_curl hub-a "$tok" POST "/api/hosts/$host/run" \
    "$(jq -nc --arg c "$cmd" '{command:$c,timeoutSeconds:10}')"
}

test_gate_refuses_disabling_firewall() {
  local tok out status body
  tok=$(token_a)
  out=$(_gate_run "$tok" remote-mid "ufw disable")
  status=$(echo "$out" | hub_status); body=$(echo "$out" | hub_body)
  assert_status "ufw disable is refused" "$status" 403
  case "$body" in *"firewall"*) pass "refusal names the firewall" ;; *) fail "refusal names the firewall (got: $body)" ;; esac
}

test_gate_refuses_cutting_ssh_access() {
  local tok out status body
  tok=$(token_a)
  out=$(_gate_run "$tok" remote-mid "systemctl stop ssh")
  status=$(echo "$out" | hub_status); body=$(echo "$out" | hub_body)
  assert_status "systemctl stop ssh is refused" "$status" 403
  case "$body" in *"SSH"*) pass "refusal names SSH access" ;; *) fail "refusal names SSH access (got: $body)" ;; esac
}

test_gate_refuses_wiping_a_disk() {
  local tok out status
  tok=$(token_a)
  out=$(_gate_run "$tok" remote-mid "mkfs.ext4 /dev/sdb")
  status=$(echo "$out" | hub_status)
  assert_status "mkfs.ext4 on a disk is refused" "$status" 403
}

test_gate_refuses_dd_to_a_device() {
  local tok out status
  tok=$(token_a)
  out=$(_gate_run "$tok" remote-mid "dd if=/dev/zero of=/dev/sdb")
  status=$(echo "$out" | hub_status)
  assert_status "dd of=/dev/sdb is refused" "$status" 403
}

test_gate_refuses_deleting_a_system_path() {
  local tok out status
  tok=$(token_a)
  out=$(_gate_run "$tok" remote-mid "rm -rf /etc")
  status=$(echo "$out" | hub_status)
  assert_status "rm -rf /etc is refused" "$status" 403
}

test_gate_refuses_rebooting() {
  local tok out status
  tok=$(token_a)
  out=$(_gate_run "$tok" remote-mid "reboot")
  status=$(echo "$out" | hub_status)
  assert_status "reboot is refused" "$status" 403
}

test_gate_refuses_editing_ssh_config_by_hand() {
  local tok out status
  tok=$(token_a)
  out=$(_gate_run "$tok" remote-mid "sed -i s/foo/bar/ /etc/ssh/sshd_config")
  status=$(echo "$out" | hub_status)
  assert_status "editing /etc/ssh/sshd_config with sed is refused" "$status" 403
}

test_gate_allows_an_ordinary_command() {
  # The gate is a floor, not a sandbox: an unrelated command must still
  # run, or every refusal above would be meaningless.
  local tok out status body
  tok=$(token_a)
  out=$(_gate_run "$tok" remote-mid "echo hello-from-the-gate-test")
  status=$(echo "$out" | hub_status); body=$(echo "$out" | hub_body)
  assert_status "an ordinary echo is allowed" "$status" 200
  case "$(echo "$body" | jq -r .stdout 2>/dev/null)" in
    *hello-from-the-gate-test*) pass "the command actually ran on the remote" ;;
    *) fail "the command actually ran on the remote (got: $body)" ;;
  esac
}

test_gate_refuses_shell_trickery_around_a_refusal() {
  # rules match on the simple command with sudo/env stripped, and a
  # `;`-joined chain is still inspected segment by segment.
  local tok out status
  tok=$(token_a)
  out=$(_gate_run "$tok" remote-mid "echo hi; iptables -F")
  status=$(echo "$out" | hub_status)
  assert_status "a refused command hidden after a ';' is still refused" "$status" 403
}

test_hub_is_not_a_host_the_gate_can_name() {
  # "the hub is not a host record, so there is no argument that means
  # 'here'." — running against a name the hub answers to itself is a 404,
  # never a 200 that executed locally.
  local tok status
  tok=$(token_a)
  status=$(_gate_run "$tok" hub-a "id" | hub_status)
  assert_status "running a command against host 'hub-a' (itself) is refused" "$status" 404
}

test_no_api_call_reaches_ssh_without_a_token() {
  local out status
  out=$("$SSH" hub-a "curl -sS -o /dev/null -w '%{http_code}' 'http://127.0.0.1:7433/api/hosts'")
  assert_status "GET /api/hosts with no Authorization header is refused" "$out" 401
}

# docs/running/safety.md — "A remote's agent is root on its own box, and
# that is all it is." These run the way a job's own shell does — root,
# the agent's home, the forwards up (agent_run in lib.sh) — and check
# what holds the three things a job may not do from outside the job.
_as_agent() { agent_run hub-a "$1" "$2" "$3"; }

test_a_job_shell_is_root_with_the_agent_home() {
  local tok body
  tok=$(token_a)
  body=$(_as_agent "$tok" remote-mid 'id -un; echo "home=$HOME"' | hub_body | jq -r .stdout)
  case "$body" in
    root*home=/home/homedash-agent*exit=0*) pass "a job's shell is root with the agent's home" ;;
    *) fail "a job's shell is root with the agent's home (got: $body)" ;;
  esac
}

test_an_older_layouts_cage_and_sudo_door_are_gone() {
  # jobs.md: Re-provision "removes what an older layout left (the network
  # cage, homedash-sudo)"; enrollment.md: the agent's account has no groups.
  local out
  out=$("$SSH" remote-mid "sudo nft list table inet homedash-agent >/dev/null 2>&1 && echo CAGE; test -e /usr/local/bin/homedash-sudo && echo SUDO-HELPER; id -nG homedash-agent | grep -qw docker && echo DOCKER-GROUP; echo done" 2>/dev/null)
  case "$out" in
    *CAGE*|*SUDO-HELPER*|*DOCKER-GROUP*) fail "no cage, no homedash-sudo, no docker group for the agent's account — re-provision remote-mid (got: $out)" ;;
    *done*) pass "no cage, no homedash-sudo, no docker group for the agent's account" ;;
    *) fail "could not read remote-mid's layout (got: $out)" ;;
  esac
}

test_the_door_serves_the_router_and_no_root() {
  # job-door.md: the door serves "exactly two things: the router ... and
  # GET /secrets/{name}". The sudo path is gone, and the hub's panel is
  # not forwarded.
  local tok body
  tok=$(token_a)
  body=$(_as_agent "$tok" remote-mid "curl -s -o /dev/null -w 'sudo=%{http_code} ' -X POST http://127.0.0.1:11435/sudo; curl -s -o /dev/null -w 'router=%{http_code} ' http://127.0.0.1:11435/api/version; curl -s -o /dev/null -w 'api=%{http_code}' http://127.0.0.1:11435/api/hosts" | hub_body | jq -r .stdout)
  case "$body" in
    *sudo=404*router=200*api=404*) pass "the door serves the router, not root, not the hub's API" ;;
    *) fail "the door serves the router, not root, not the hub's API (got: $body)" ;;
  esac
}

test_a_job_round_puts_back_the_hubs_hold() {
  # safety.md: "The hub's hold is put back after every round ... and
  # records it as a host.hold_repaired event." Change the hub's sudoers
  # line (still valid, so the round can start), run a trivial job, and
  # read the line back.
  local tok want got n0 id
  tok=$(token_a)
  want='homedash ALL=(ALL) NOPASSWD:ALL'
  n0=$(event_count hub-a "$tok" host.hold_repaired remote-mid)
  "$SSH" remote-mid "sudo chattr -i /etc/sudoers.d/homedash; printf '%s\n# tests\n' '$want' | sudo tee /etc/sudoers.d/homedash >/dev/null" 2>/dev/null
  id=$(_start_job "$tok" remote-mid "Say the single word: ack")
  [ -n "$id" ] && [ "$id" != null ] && _wait_job "$tok" "$id" >/dev/null
  got=$("$SSH" remote-mid "sudo cat /etc/sudoers.d/homedash" 2>/dev/null)
  assert_eq "the hub's sudoers line is put back after the round" "$got" "$want"
  assert_true "the restore is a host.hold_repaired event" test "$(event_count hub-a "$tok" host.hold_repaired remote-mid)" -gt "$n0"
  # Leave the machine as the suite found it whatever happened above.
  [ "$got" = "$want" ] || "$SSH" remote-mid "sudo chattr -i /etc/sudoers.d/homedash; echo '$want' | sudo tee /etc/sudoers.d/homedash >/dev/null; sudo chattr +i /etc/sudoers.d/homedash" 2>/dev/null
}

test_the_gate_publishes_its_list_of_refusals() {
  # safety.md's second refusal "is a list", and the CLI docs say to
  # "point it at safety to know what will be refused and why" — the list
  # is readable at the API, and each item in the doc is on it.
  local tok body
  tok=$(token_a)
  body=$(hub_curl hub-a "$tok" GET /api/gate | hub_body | tr '\n' ' ')
  for want in firewall "SSH access" "SSH config" disk "system path" reboot; do
    case "$body" in
      *"$want"*) pass "the gate's list names: $want" ;;
      *) fail "the gate's list names: $want (got: $body)" ;;
    esac
  done
}

test_writing_a_file_goes_through_the_same_gate() {
  # "Every call that can reach SSH ... passes one guard first." A file
  # write into SSH's config is editing SSH config by hand, however it is
  # asked for.
  local tok status
  tok=$(token_a)
  status=$(hub_curl hub-a "$tok" PUT /api/hosts/remote-mid/file \
    '{"path":"/etc/ssh/sshd_config.d/tests.conf","content":"PermitRootLogin yes\n","sudo":true}' | hub_status)
  assert_status "writing under /etc/ssh is refused" "$status" 403
  status=$(hub_curl hub-a "$tok" PUT /api/hosts/remote-mid/file \
    '{"path":"/tmp/tests-ordinary-file","content":"fine\n"}' | hub_status)
  assert_status "an ordinary file write is allowed" "$status" 204
  hub_curl hub-a "$tok" POST /api/hosts/remote-mid/run '{"command":"rm -f /tmp/tests-ordinary-file","timeoutSeconds":10}' >/dev/null
}

test_every_gate_refusal_is_an_event() {
  # "the refusal is what the agent sees, and what lands in the event log."
  local tok n0
  tok=$(token_a)
  n0=$(event_count hub-a "$tok" gate.refused "")
  _gate_run "$tok" remote-mid "reboot" >/dev/null
  assert_true "a refused command lands in the event log" test "$(event_count hub-a "$tok" gate.refused "")" -gt "$n0"
}

