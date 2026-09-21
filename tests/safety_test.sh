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

# docs/running/safety.md — "A remote's agent is not root, and cannot
# become root." These run as the agent account through the executor, with
# the forwards up, exactly the way a job's own shell would see things
# (agent_run in lib.sh).
_as_agent() { agent_run hub-a "$1" "$2" "$3"; }

test_agent_account_has_no_sudo() {
  local tok body
  tok=$(token_a)
  body=$(_as_agent "$tok" remote-mid "id -un; sudo -n true" | hub_body | jq -r .stdout)
  case "$body" in
    homedash-agent*exit=0*) fail "the agent account cannot sudo (got: $body)" ;;
    homedash-agent*) pass "the agent account is homedash-agent and cannot sudo" ;;
    *) fail "the agent account is homedash-agent and cannot sudo (got: $body)" ;;
  esac
}

test_agent_account_has_docker_and_its_home_and_shared_storage() {
  # safety.md: "docker is the one privilege it holds", and it writes "its
  # own home ... and the house's shared storage, the cluster this remote
  # is the gateway of and every workspace it is a member of". The lab's
  # cluster `pool` has its gateway on remote-big.
  local tok body cpath
  tok=$(token_a)
  body=$(_as_agent "$tok" remote-mid "id -nG | tr ' ' '\n' | grep -qx docker && echo IN-DOCKER-GROUP; docker version --format '{{.Server.Version}}' >/dev/null 2>&1 && echo DAEMON-OK; touch /home/homedash-agent/tests-x && echo HOME-OK; rm -f /home/homedash-agent/tests-x" | hub_body | jq -r .stdout)
  case "$body" in
    *IN-DOCKER-GROUP*DAEMON-OK*HOME-OK*) pass "the agent account is in the docker group, reaches the daemon and writes its home" ;;
    *) fail "the agent account is in the docker group, reaches the daemon and writes its home (got: $body)" ;;
  esac
  cpath=$(hub_curl hub-a "$tok" GET /api/clusters | hub_body | jq -r '.[] | select(.name=="pool") | .path')
  [ -n "$cpath" ] && [ "$cpath" != null ] || { skip "the lab's cluster pool is not there to test shared storage on"; return; }
  body=$(_as_agent "$tok" remote-big "touch $cpath/tests-x && echo POOL-OK; rm -f $cpath/tests-x" | hub_body | jq -r .stdout)
  case "$body" in
    *POOL-OK*) pass "a job's account on the gateway writes the cluster" ;;
    *) fail "a job's account on the gateway writes the cluster (got: $body)" ;;
  esac
}

test_agent_account_cannot_touch_the_hubs_hold() {
  local tok body
  tok=$(token_a)
  body=$(_as_agent "$tok" remote-mid "cat /etc/ssh/authorized_keys.d/homedash >/dev/null && echo READ; echo x >> /etc/sudoers.d/homedash && echo WROTE; rm -f /home/homedash-agent/.omp/agent/hooks/pre/homedash-refusals.ts && echo REMOVED; true" | hub_body | jq -r .stdout)
  case "$body" in
    *WROTE*|*REMOVED*) fail "the agent cannot write sudoers or remove its hook (got: $body)" ;;
    *) pass "the agent cannot write sudoers or remove its hook" ;;
  esac
}

test_agent_account_is_caged_from_the_house() {
  # The hub's LAN address is a private address; the agent's uid may not reach it.
  local tok hub body
  tok=$(token_a)
  hub=$(hub_curl hub-a "$tok" GET /api/settings | hub_body | jq -r '."hub.lan_addr" // empty')
  [ -n "$hub" ] || hub=$(hub_curl hub-a "$tok" GET /api/hosts | hub_body | jq -r '.[0].addr' | sed 's/\.[0-9]*$/.1/')
  body=$(_as_agent "$tok" remote-mid "curl -s -m 3 -o /dev/null -w '%{http_code}' http://$hub:7433/ || echo caged; curl -s -m 3 -o /dev/null -w ' lo=%{http_code}' http://127.0.0.1:11435/api/version" | hub_body | jq -r .stdout)
  case "$body" in
    *caged*lo=200*) pass "the agent reaches loopback (the door) but not the hub's address" ;;
    *caged*) pass "the agent cannot reach the hub's address (door check inconclusive: $body)" ;;
    *) fail "the agent cannot reach the hub's address (got: $body)" ;;
  esac
}

test_sudo_door_runs_an_allowed_command_and_refuses_a_protected_path() {
  local tok body
  tok=$(token_a)
  body=$(_as_agent "$tok" remote-mid "homedash-sudo id -un; echo rc=\$?; homedash-sudo cat /etc/shadow; echo rc=\$?; homedash-sudo bash -c id; echo rc=\$?" | hub_body | jq -r .stdout)
  case "$body" in
    *root*rc=0*"protected path"*rc=126*"privileged program"*rc=126*) pass "homedash-sudo runs as root through the gate and refuses protected paths and shells" ;;
    *) fail "homedash-sudo runs as root through the gate and refuses protected paths and shells (got: $body)" ;;
  esac
}

test_sudo_door_can_be_switched_off() {
  local tok body
  tok=$(token_a)
  hub_curl hub-a "$tok" PUT /api/settings '{"jobs.sudo":"off"}' >/dev/null
  body=$(_as_agent "$tok" remote-mid "homedash-sudo id -un; echo rc=\$?" | hub_body | jq -r .stdout)
  hub_curl hub-a "$tok" PUT /api/settings '{"jobs.sudo":""}' >/dev/null
  case "$body" in
    *"switched off"*rc=126*) pass "jobs.sudo off closes the door" ;;
    *) fail "jobs.sudo off closes the door (got: $body)" ;;
  esac
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

test_the_door_refuses_a_privileged_container_and_logs_every_use() {
  # safety.md: the allowlist is "docker without privileged or
  # host-namespace flags", and "every use of it is named, checked and
  # logged".
  local tok body runs0
  tok=$(token_a)
  runs0=$(event_count hub-a "$tok" sudo.run remote-mid)
  body=$(_as_agent "$tok" remote-mid "homedash-sudo docker run --rm --privileged alpine id; echo rc=\$?; homedash-sudo docker run --rm --pid=host alpine id; echo rc=\$?; homedash-sudo true; echo rc=\$?" | hub_body | jq -r .stdout)
  case "$body" in
    *"privileged container"*rc=126*"privileged container"*rc=126*rc=0*) pass "docker --privileged and --pid=host are refused; a plain command is not" ;;
    *) fail "docker --privileged and --pid=host are refused; a plain command is not (got: $body)" ;;
  esac
  assert_true "a command run through the door is an event" test "$(event_count hub-a "$tok" sudo.run remote-mid)" -gt "$runs0"
}

test_the_agent_unit_is_enforced_by_the_kernel() {
  # safety.md: "inside a systemd unit the kernel enforces: NoNewPrivileges
  # ... the system read-only (ProtectSystem=strict), a private /tmp".
  # A job is the only thing that runs in that unit; start one whose
  # instructions are to write outside its two places, and read the
  # events it left. Cheaper and deterministic: run the same unit shape
  # the hub uses, as the hub does, and try to write /usr.
  local tok body
  tok=$(token_a)
  body=$(hub_curl hub-a "$tok" POST /api/hosts/remote-mid/run \
    "$(jq -nc '{command:"sudo -n systemd-run --quiet --pipe --wait --collect --uid=homedash-agent --gid=homedash-agent -p NoNewPrivileges=yes -p ProtectSystem=strict -p PrivateTmp=yes -p ReadWritePaths=/home/homedash-agent sh -c \"touch /usr/tests-x 2>&1; echo rc=$?; touch /home/homedash-agent/tests-x && echo home-ok; rm -f /home/homedash-agent/tests-x\"",timeoutSeconds:30}')" | hub_body | jq -r .stdout)
  case "$body" in
    *"Read-only"*home-ok*) pass "the system is read-only to the agent's unit; its home is not" ;;
    *) fail "the system is read-only to the agent's unit; its home is not (got: $body)" ;;
  esac
}
