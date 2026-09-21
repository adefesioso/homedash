# docs/pooling/storage.md#a-shared-workspace — a directory on a cluster
# that appears at the same path on every remote you name, so jobs on
# different machines work the same files; removing a remote closes the
# share and the files stay.
#
# Built on the lab's existing "pool" cluster (gateway remote-big) and
# shared to remote-mid and remote-small — the two remotes that are not
# the gateway, so the share really crosses a machine boundary.

_WS=tests-ws
_ws_row() { hub_curl hub-a "$(token_a)" GET /api/workspaces | hub_body | jq -c --arg n "$_WS" '.[] | select(.name==$n)'; }
_run() { # _run <host> <cmd> -> stdout
  hub_curl hub-a "$(token_a)" POST "/api/hosts/$1/run" "$(jq -nc --arg c "$2" '{command:$c,timeoutSeconds:30}')" | hub_body | jq -r '.stdout // empty'
}
_mounted_on() { [ "$(_ws_row | jq -r --arg h "$1" '.mounted[$h]')" = "true" ]; }
_not_mounted_on() { ! _mounted_on "$1"; }

_ws_cleanup() {
  local id
  id=$(_ws_row | jq -r '.id // empty')
  [ -n "$id" ] && hub_curl hub-a "$(token_a)" DELETE "/api/workspaces/$id" >/dev/null
  local cpath
  cpath=$(hub_curl hub-a "$(token_a)" GET /api/clusters | hub_body | jq -r '.[] | select(.cluster.name=="pool") | .cluster.path')
  [ -n "$cpath" ] && _run remote-big "sudo rm -rf $cpath/$_WS" >/dev/null
}

test_a_workspace_appears_at_the_same_path_on_every_named_remote() {
  local tok out status path
  tok=$(token_a)
  _ws_cleanup
  out=$(hub_curl hub-a "$tok" POST /api/workspaces \
    "$(jq -nc --arg n "$_WS" '{name:$n,cluster:"pool",hosts:["remote-mid","remote-small"]}')")
  status=$(echo "$out" | hub_status)
  assert_status "a workspace is created on the pool cluster" "$status" 200
  path=$(echo "$out" | hub_body | jq -r .path)
  assert_true "the workspace has one path" test -n "$path" -a "$path" != "null"
  assert_eq "it names both remotes" "$(echo "$out" | hub_body | jq -r '[.members[].host] | sort | join(" ")')" "remote-mid remote-small"

  assert_true "remote-mid reports the path mounted" wait_until 40 _mounted_on remote-mid
  assert_true "remote-small reports the path mounted" wait_until 40 _mounted_on remote-small
}

test_a_file_one_remote_writes_the_other_reads() {
  local path got
  path=$(_ws_row | jq -r .path)
  _run remote-mid "echo hello-from-mid > $path/tests-file" >/dev/null
  got=$(_run remote-small "cat $path/tests-file")
  assert_eq "a file written on remote-mid is read on remote-small at the same path" "$got" "hello-from-mid"
}

test_removing_a_remote_closes_its_share_and_the_files_stay() {
  local tok id path status
  tok=$(token_a)
  id=$(_ws_row | jq -r .id)
  path=$(_ws_row | jq -r .path)
  status=$(hub_curl hub-a "$tok" DELETE "/api/workspaces/$id/members/remote-small" | hub_status)
  assert_status "remote-small is removed from the workspace" "$status" 204
  assert_eq "the workspace now names only remote-mid" "$(_ws_row | jq -r '[.members[].host] | join(" ")')" "remote-mid"
  assert_true "remote-small no longer has the path mounted" wait_until 30 _not_mounted_on remote-small
  assert_eq "the file is still there for remote-mid" "$(_run remote-mid "cat $path/tests-file")" "hello-from-mid"
}

test_deleting_the_workspace_leaves_the_files_on_the_cluster() {
  local tok id path status
  tok=$(token_a)
  id=$(_ws_row | jq -r .id)
  path=$(_ws_row | jq -r .path)
  status=$(hub_curl hub-a "$tok" DELETE "/api/workspaces/$id" | hub_status)
  assert_status "the workspace is deleted" "$status" 204
  assert_eq "it is gone from the list" "$(_ws_row)" ""
  # The gateway is where the cluster's path is; the directory is still
  # on it — "the files stay".
  assert_eq "the files are still on the cluster at the gateway" "$(_run remote-big "cat $path/tests-file")" "hello-from-mid"
  _ws_cleanup
}
