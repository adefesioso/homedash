# docs/pooling/storage.md — a cluster is several disks presented as one
# path with capacities added, and a member that's off is a hole, not a
# hang. This lab's disks are already fully committed to the "pool"
# cluster built by an earlier session (remote-big:/mnt/sdb+sdc,
# remote-mid:/mnt/sdb), so these tests read and lightly exercise that
# live cluster instead of building a throwaway one from spare disks —
# there are none in this lab's fixed VM table (lab/lab.env).

_pool_cluster() { hub_curl hub-a "$(token_a)" GET /api/clusters | hub_body | jq -c '.[] | select(.cluster.name=="pool")'; }

test_cluster_capacity_is_the_members_added_together() {
  local row size sum
  row=$(_pool_cluster)
  size=$(echo "$row" | jq -r '.size')
  sum=$(echo "$row" | jq -r '[.members[].size] | add')
  assert_eq "the cluster's total size is exactly its members' sizes added" "$size" "$sum"
}

test_cluster_is_two_or_more_members_on_a_gateway() {
  local row n gw
  row=$(_pool_cluster)
  n=$(echo "$row" | jq -r '.members | length')
  gw=$(echo "$row" | jq -r '.cluster.gateway')
  assert_true "the cluster has two or more members" test "$n" -ge 2
  assert_eq "the gateway is remote-big, where the path appears" "$gw" "remote-big"
}

test_cluster_reads_healthy_while_every_member_is_reachable() {
  local row degraded unreachable
  row=$(_pool_cluster)
  degraded=$(echo "$row" | jq -r '.degraded')
  unreachable=$(echo "$row" | jq -r '[.members[] | select(.reachable==false)] | length')
  assert_eq "no member currently reads unreachable" "$unreachable" "0"
  assert_eq "the cluster does not read Degraded" "$degraded" "false"
}

test_a_member_carrying_the_system_disk_is_refused() {
  # docs/pooling/storage.md + running/safety.md: pooling a disk that
  # carries the system, swap or the cluster's own mount is refused in
  # code.
  local tok status
  tok=$(token_a)
  status=$(hub_curl hub-a "$tok" POST /api/clusters \
    '{"name":"tests-bad-pool","gateway":"remote-big","path":"/mnt/tests-bad-pool","members":[{"host":"remote-mid","path":"/"}]}' | hub_status)
  assert_status "pooling / (the system disk) is refused" "$status" 400
}

test_removing_then_readding_a_member_leaves_the_cluster_working() {
  # Exercises "removing a member rebuilds the mount without it" on the
  # live cluster, then restores it exactly as it was.
  local tok cid mid_member_id before after
  tok=$(token_a)
  cid=$(_pool_cluster | jq -r '.cluster.id')
  mid_member_id=$(_pool_cluster | jq -r '.cluster.members[] | select(.host=="remote-mid") | .id')
  before=$(_pool_cluster | jq -r '.members | length')

  assert_status "removing remote-mid's member succeeds" \
    "$(hub_curl hub-a "$tok" DELETE "/api/clusters/$cid/members/$mid_member_id" | hub_status)" 204

  local mid_count
  mid_count=$(_pool_cluster | jq -r '.members | length')
  assert_true "the cluster now has one fewer member" test "$mid_count" -lt "$before"
  assert_eq "the cluster still mounts and reads healthy without it (a hole, not a hang)" \
    "$(_pool_cluster | jq -r '.mounted')" "true"

  assert_status "re-adding remote-mid:/mnt/sdb succeeds" \
    "$(hub_curl hub-a "$tok" POST "/api/clusters/$cid/members" '{"host":"remote-mid","path":"/mnt/sdb"}' | hub_status)" 204
  after=$(_pool_cluster | jq -r '.members | length')
  assert_eq "the cluster is back to its original member count" "$after" "$before"
}

test_a_member_that_is_off_is_a_hole_not_a_hang() {
  # docs/pooling/storage.md: "A member that's off is a hole in the
  # namespace — its files are missing until it returns, and everything
  # else still reads. A dead member returns an error instead of hanging
  # ... The cluster reads Degraded and names the member." remote-mid is
  # a member; stop its VM on Proxmox, read the mount on the gateway with
  # a deadline, and bring it back before the suite moves on.
  local tok vmid out
  tok=$(token_a)
  # shellcheck source=/dev/null
  . "$LAB_DIR/lab.env"
  vmid=$(echo "$VMS" | awk '$2=="remote-mid"{print $1}')
  if [ -z "$vmid" ]; then skip "no VM id for remote-mid in lab.env"; return; fi
  "$SSH" pve "qm stop $vmid" >/dev/null 2>&1
  trap '"$SSH" pve "qm start '"$vmid"'" >/dev/null 2>&1' RETURN

  _degraded() { [ "$(_pool_cluster | jq -r '.degraded')" = "true" ]; }
  assert_true "the cluster reads Degraded" wait_until 180 _degraded
  case "$(_pool_cluster | jq -r '[.members[] | select(.reachable==false) | .host] | join(" ")')" in
    *remote-mid*) pass "and names the member that is off" ;;
    *) fail "and names the member that is off" ;;
  esac
  out=$(hub_curl hub-a "$tok" POST /api/hosts/remote-big/run \
    "$(jq -nc --arg p "$(_pool_cluster | jq -r '.cluster.path')" '{command:("timeout 20 ls " + $p + " >/dev/null; echo rc=$?"),timeoutSeconds:40}')" | hub_body | jq -r .stdout)
  case "$out" in
    *rc=0*) pass "the mount on the gateway still reads with the member off (a hole, not a hang)" ;;
    *rc=124*) fail "listing the mount hung past 20 s with a member off" ;;
    *) fail "the mount on the gateway still reads with the member off (got: $out)" ;;
  esac

  "$SSH" pve "qm start $vmid" >/dev/null 2>&1
  trap - RETURN
  _mid_online() { [ "$(hub_curl hub-a "$tok" GET /api/hosts | hub_body | jq -r '.[] | select(.name=="remote-mid") | .status')" = "online" ]; }
  assert_true "remote-mid comes back online" wait_until 240 _mid_online
  _healthy() { [ "$(_pool_cluster | jq -r '.degraded')" = "false" ]; }
  assert_true "the cluster reads healthy again once it returns" wait_until 180 _healthy
}
