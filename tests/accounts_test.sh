# docs/running/accounts.md — two roles, "enforced at the API rather than
# by hiding buttons."

test_viewer_token_can_read() {
  local admin_tok viewer_tok status
  admin_tok=$(token_a)
  viewer_tok=$(hub_curl hub-a "$admin_tok" POST /api/tokens \
    '{"name":"tests-viewer","role":"viewer"}' | hub_body | jq -r .token)
  assert_true "a viewer token was minted" test -n "$viewer_tok"
  status=$(hub_curl hub-a "$viewer_tok" GET /api/hosts | hub_status)
  assert_status "a viewer can GET /api/hosts" "$status" 200
  hub_curl hub-a "$admin_tok" DELETE /api/tokens/tests-viewer >/dev/null
}

test_viewer_token_cannot_write() {
  local admin_tok viewer_tok status
  admin_tok=$(token_a)
  viewer_tok=$(hub_curl hub-a "$admin_tok" POST /api/tokens \
    '{"name":"tests-viewer2","role":"viewer"}' | hub_body | jq -r .token)
  status=$(hub_curl hub-a "$viewer_tok" POST /api/tasks \
    '{"name":"should-not-be-created","hostId":0,"command":"echo hi","schedule":"@daily","timeoutSeconds":60,"enabled":true}' \
    | hub_status)
  assert_status "a viewer's POST /api/tasks is refused (403, not silently allowed)" "$status" 403
  hub_curl hub-a "$admin_tok" DELETE /api/tokens/tests-viewer2 >/dev/null
}

test_viewer_may_use_the_router() {
  # docs/running/accounts.md + auth.go's viewerMayPost: "the router is a
  # read for a viewer (a prompt changes nothing about the lab)".
  local admin_tok viewer_tok status
  admin_tok=$(token_a)
  viewer_tok=$(hub_curl hub-a "$admin_tok" POST /api/tokens \
    '{"name":"tests-viewer3","role":"viewer"}' | hub_body | jq -r .token)
  status=$(hub_curl hub-a "$viewer_tok" POST /api/generate \
    '{"model":"qwen2.5:0.5b","prompt":"say hi","stream":false}' | hub_status)
  case "$status" in
    200|400|404|502) pass "a viewer's POST /api/generate is not blocked by role (got $status, a router-level answer not a 403)" ;;
    403) fail "a viewer's POST /api/generate should not be a role refusal (got 403)" ;;
    *) fail "unexpected status $status from /api/generate as viewer" ;;
  esac
  hub_curl hub-a "$admin_tok" DELETE /api/tokens/tests-viewer3 >/dev/null
}

test_an_admin_invites_with_a_single_use_code_and_a_viewer_cannot() {
  # accounts.md: "registration is closed and an admin invites the rest
  # with a single-use code" — an act of the admin role, refused to a
  # viewer at the API.
  local admin_tok viewer_tok body code listed status
  admin_tok=$(token_a)
  body=$(hub_curl hub-a "$admin_tok" POST /api/invites '{"role":"viewer"}' | hub_body)
  code=$(echo "$body" | jq -r '.code // empty')
  assert_true "an admin mints an invite code" test -n "$code"
  assert_eq "the invite carries the role it grants" "$(echo "$body" | jq -r .role)" "viewer"
  listed=$(hub_curl hub-a "$admin_tok" GET /api/invites | hub_body | jq -r --arg c "$code" '.[] | select(.code==$c) | .code')
  assert_eq "the open invite is listed until it is used" "$listed" "$code"

  viewer_tok=$(hub_curl hub-a "$admin_tok" POST /api/tokens '{"name":"tests-viewer4","role":"viewer"}' | hub_body | jq -r .token)
  status=$(hub_curl hub-a "$viewer_tok" POST /api/invites '{"role":"admin"}' | hub_status)
  assert_status "a viewer cannot invite" "$status" 403
  hub_curl hub-a "$admin_tok" DELETE /api/tokens/tests-viewer4 >/dev/null
}

test_an_api_token_is_shown_once() {
  # accounts.md: "an API token instead, made in Settings or from that
  # same shell, shown once."
  local admin_tok tok listed
  admin_tok=$(token_a)
  tok=$(hub_curl hub-a "$admin_tok" POST /api/tokens '{"name":"tests-once","role":"viewer"}' | hub_body | jq -r .token)
  assert_true "the token is returned when made" test -n "$tok" -a "$tok" != "null"
  listed=$(hub_curl hub-a "$admin_tok" GET /api/tokens | hub_body)
  case "$listed" in
    *"$tok"*) fail "the list never shows the token again" ;;
    *) pass "the list never shows the token again" ;;
  esac
  case "$listed" in
    *tests-once*) pass "the list shows its name and role" ;;
    *) fail "the list shows its name and role" ;;
  esac
  hub_curl hub-a "$admin_tok" DELETE /api/tokens/tests-once >/dev/null
  assert_status "a deleted token opens nothing" "$(hub_curl hub-a "$tok" GET /api/hosts | hub_status)" 401
}

test_a_duplicate_token_name_is_refused() {
  # docs/running/accounts.md: "A token name is unique; revoking it
  # revokes that token, and only that one." (C-6)
  local admin_tok status resp body
  admin_tok=$(token_a)
  status=$(hub_curl hub-a "$admin_tok" POST /api/tokens '{"name":"tests-dup","role":"viewer"}' | hub_status)
  assert_status "the first token named tests-dup is made" "$status" 200
  resp=$(hub_curl hub-a "$admin_tok" POST /api/tokens '{"name":"tests-dup","role":"viewer"}')
  status=$(echo "$resp" | hub_status)
  body=$(echo "$resp" | hub_body)
  assert_status "a second token named tests-dup is refused" "$status" 409
  case "$body" in
    *"tests-dup"*"exists"*) pass "the 409 names the token and says to revoke it first" ;;
    *) fail "the 409 body should name the token and say to revoke it (got: $body)" ;;
  esac
  hub_curl hub-a "$admin_tok" DELETE /api/tokens/tests-dup >/dev/null
}

test_a_revoked_invite_cannot_be_used() {
  # docs/running/accounts.md: "an invite can be revoked before it is
  # used." (C-4)
  local admin_tok code status listed
  admin_tok=$(token_a)
  code=$(hub_curl hub-a "$admin_tok" POST /api/invites '{"role":"viewer"}' | hub_body | jq -r .code)
  assert_true "an invite is minted" test -n "$code"
  status=$(hub_curl hub-a "$admin_tok" DELETE "/api/invites/$code" | hub_status)
  assert_status "the invite is revoked" "$status" 204
  listed=$(hub_curl hub-a "$admin_tok" GET /api/invites | hub_body | jq -r --arg c "$code" '.[] | select(.code==$c) | .code')
  assert_eq "a revoked invite is no longer listed" "$listed" ""
  status=$(hub_curl hub-a "" POST /api/auth/register/begin "$(jq -nc --arg c "$code" '{name:"tests-revoked",invite:$c}')" | hub_status)
  assert_status "register/begin with a revoked code is refused" "$status" 400
}

test_the_last_admin_cannot_be_demoted() {
  # docs/running/accounts.md: "Demoting an admin to viewer is refused
  # only when it is the last one." (C-5) The lab's hubs have exactly one
  # admin account, so that admin is demoted (via a token, which is never
  # "yourself") and the guard must still refuse it.
  local admin_tok admins name status role
  admin_tok=$(token_a)
  admins=$(hub_curl hub-a "$admin_tok" GET /api/users | hub_body | jq -r '[.[] | select(.role=="admin")] | length')
  if [ "$admins" != "1" ]; then
    skip "the last-admin guard needs exactly one admin on hub-a (found $admins)"
    return
  fi
  name=$(hub_curl hub-a "$admin_tok" GET /api/users | hub_body | jq -r '.[] | select(.role=="admin") | .name')
  status=$(hub_curl hub-a "$admin_tok" PUT "/api/users/$name/role" '{"role":"viewer"}' | hub_status)
  assert_status "demoting the only admin is refused" "$status" 400
  role=$(hub_curl hub-a "$admin_tok" GET /api/users | hub_body | jq -r --arg n "$name" '.[] | select(.name==$n) | .role')
  assert_eq "the admin's role is unchanged" "$role" "admin"
}

test_agent_default_model_allows_a_second_slash() {
  # docs/pooling/agents/README.md: "A model is always provider/model —
  # the model half is the provider's own id and may itself contain
  # slashes (openrouter/openai/gpt-4o-mini)." agentModelRE in server.go
  # constrains only the leading provider segment.
  local admin_tok prev status got
  admin_tok=$(token_a)
  prev=$(hub_curl hub-a "$admin_tok" GET /api/settings | hub_body | jq -r '."agent.default_model"')
  status=$(hub_curl hub-a "$admin_tok" PUT /api/settings '{"agent.default_model":"openrouter/openai/gpt-4o-mini"}' | hub_status)
  assert_status "a provider/vendor/model value is accepted" "$status" 200
  got=$(hub_curl hub-a "$admin_tok" GET /api/settings | hub_body | jq -r '."agent.default_model"')
  assert_eq "the two-slash model round-trips" "$got" "openrouter/openai/gpt-4o-mini"
  hub_curl hub-a "$admin_tok" PUT /api/settings "$(jq -nc --arg m "$prev" '{"agent.default_model":$m}')" >/dev/null
}

test_agent_remote_model_is_its_own_setting() {
  # hub/internal/agent/README.md: windows run agent.default_model, a
  # remote's job runs its card override, else agent.remote_model, else
  # the hub's model. The key is validated the same way and stored apart.
  local admin_tok prev hub_prev status got
  admin_tok=$(token_a)
  prev=$(hub_curl hub-a "$admin_tok" GET /api/settings | hub_body | jq -r '."agent.remote_model"')
  hub_prev=$(hub_curl hub-a "$admin_tok" GET /api/settings | hub_body | jq -r '."agent.default_model"')
  status=$(hub_curl hub-a "$admin_tok" PUT /api/settings '{"agent.remote_model":"no-slash"}' | hub_status)
  assert_status "a remote model without provider/ is refused" "$status" 400
  status=$(hub_curl hub-a "$admin_tok" PUT /api/settings '{"agent.remote_model":"openrouter/openai/gpt-4o-mini"}' | hub_status)
  assert_status "a remote model is accepted" "$status" 200
  got=$(hub_curl hub-a "$admin_tok" GET /api/settings | hub_body | jq -r '."agent.default_model"')
  assert_eq "the hub model is untouched by the remote one" "$got" "$hub_prev"
  hub_curl hub-a "$admin_tok" PUT /api/settings "$(jq -nc --arg m "$prev" '{"agent.remote_model":$m}')" >/dev/null
}

test_every_account_has_one_of_two_roles() {
  local admin_tok roles
  admin_tok=$(token_a)
  roles=$(hub_curl hub-a "$admin_tok" GET /api/users | hub_body | jq -r '[.[].role] | unique | join(" ")')
  assert_true "the accounts list has at least one admin" bash -c "case ' $roles ' in *' admin '*) exit 0;; *) exit 1;; esac"
  assert_true "no role other than admin or viewer exists" \
    bash -c "for r in $roles; do [ \"\$r\" = admin ] || [ \"\$r\" = viewer ] || exit 1; done"
}
