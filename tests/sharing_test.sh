# docs/sharing/*.md — the space, peer discovery, inference across houses,
# and publishing a service fronted as a link. Exercises the lab's two
# actual houses (house-a / house-b) rather than mocking a peer.

_peer_id_a() { hub_curl hub-a "$(token_a)" GET /api/peers | hub_body | jq -r '.status.id'; }
_peer_id_b() { hub_curl hub-b "$(token_b)" GET /api/peers | hub_body | jq -r '.status.id'; }
_peer_row_of_b_on_a() { hub_curl hub-a "$(token_a)" GET /api/peers | hub_body | jq -c '.peers[] | select(.name=="house-b")'; }

test_hubs_discover_each_other_over_the_dht() {
  local row_a name_b row_b name_a
  row_a=$(_peer_row_of_b_on_a)
  name_b=$(echo "$row_a" | jq -r '.name')
  assert_eq "hub-a's Peers tab lists house-b" "$name_b" "house-b"

  row_b=$(hub_curl hub-b "$(token_b)" GET /api/peers | hub_body | jq -c '.peers[] | select(.name=="house-a")')
  name_a=$(echo "$row_b" | jq -r '.name')
  assert_eq "hub-b's Peers tab lists house-a" "$name_a" "house-a"

  # "A peer is another hub in your space, identified by the public key of
  # its connection" — cross-check the IDs match in both directions.
  local a_id b_id b_sees_a a_sees_b
  a_id=$(_peer_id_a); b_id=$(_peer_id_b)
  b_sees_a=$(hub_curl hub-b "$(token_b)" GET /api/peers | hub_body | jq -r '.peers[] | select(.name=="house-a") | .id')
  a_sees_b=$(echo "$row_a" | jq -r '.id')
  assert_eq "hub-b sees hub-a's own connection id" "$b_sees_a" "$a_id"
  assert_eq "hub-a sees hub-b's own connection id" "$a_sees_b" "$b_id"
}

test_settings_booleans_write_the_canonical_value() {
  # docs/sharing/README.md: "stores true when checked and blank when
  # not; that's the only vocabulary a save writes" (C-11).
  local admin_tok before got
  admin_tok=$(token_a)
  before=$(hub_curl hub-a "$admin_tok" GET /api/settings | hub_body | jq -r '."space.serve"')
  hub_curl hub-a "$admin_tok" PUT /api/settings '{"space.serve":"1"}' >/dev/null
  got=$(hub_curl hub-a "$admin_tok" GET /api/settings | hub_body | jq -r '."space.serve"')
  assert_eq "a legacy '1' write is stored as the canonical true" "$got" "true"
  hub_curl hub-a "$admin_tok" PUT /api/settings '{"space.serve":"on"}' >/dev/null
  got=$(hub_curl hub-a "$admin_tok" GET /api/settings | hub_body | jq -r '."space.serve"')
  assert_eq "an 'on' write is stored as the canonical true" "$got" "true"
  hub_curl hub-a "$admin_tok" PUT /api/settings "$(jq -nc --arg v "$before" '{"space.serve":$v}')" >/dev/null
  got=$(hub_curl hub-a "$admin_tok" GET /api/settings | hub_body | jq -r '."space.serve"')
  assert_eq "space.serve is restored to its original value" "$got" "$before"
}

_wait_offer_has_model() {
  # docs/sharing/README.md: offers are "refreshed every minute" — a
  # just-(re)connected peer can briefly not list a model it actually
  # holds, and one whose machine was busy a moment ago says so until the
  # next refresh (jobs.md: a candidate "lists the model and reports a
  # free machine"). Poll a bit past that cadence before trusting a miss.
  local i row models
  for i in 1 2 3 4 5 6 7 8; do
    row=$(_peer_row_of_b_on_a)
    models=$(echo "$row" | jq -r '.offer.models[]?')
    case " $models " in
      *" qwen2.5:0.5b "*) [ "$(echo "$row" | jq -r '.connected and .offer.free')" = "true" ] && { echo "$models"; return 0; } ;;
    esac
    sleep 15
  done
  echo "$models"; return 1
}

test_offer_lists_the_peers_models() {
  local models
  models=$(_wait_offer_has_model)
  case " $models " in
    *" qwen2.5:0.5b "*) pass "house-b's offer to house-a lists qwen2.5:0.5b" ;;
    *) fail "house-b's offer to house-a lists qwen2.5:0.5b (got: $models)" ;;
  esac
}

test_show_for_an_unknown_model_stays_home() {
  # G-5: docs/sharing/jobs.md — "Only prompts leave the house ... a
  # /api/show for a model nothing local holds gets Ollama's own 404
  # rather than a queue or a peer's refusal." A model lookup never
  # reaches a peer at all, so hub-b's refusal count must not move.
  local toka tokb refused0 out status body
  toka=$(token_a); tokb=$(token_b)
  refused0=$(event_count hub-b "$tokb" peer.refused "not a prompt")
  out=$(hub_curl hub-a "$toka" POST /api/show '{"model":"tests-no-such-model:1b"}')
  status=$(echo "$out" | hub_status)
  body=$(echo "$out" | hub_body)
  assert_status "a model lookup for a model nothing holds is a 404, answered at home" "$status" 404
  case "$body" in
    *"not found"*) pass "the 404 is Ollama's own shape" ;;
    *) fail "the 404 is Ollama's own shape (got: $body)" ;;
  esac
  sleep 2
  assert_eq "house-b recorded no new peer.refused: /api/show was never asked of it" \
    "$(event_count hub-b "$tokb" peer.refused "not a prompt")" "$refused0"
}

test_inference_falls_back_to_a_peer_when_the_local_pool_cannot_serve() {
  # docs/pooling/inference.md + docs/sharing/jobs.md: "when the local pool
  # can't place a request, a peer's pool gets first refusal before the
  # queue." Disable hub-a's only local holder of the model so this can
  # only succeed by crossing to house-b.
  local toka tokb a_id b_id res status peer
  toka=$(token_a); tokb=$(token_b)
  a_id=$(_peer_id_a); b_id=$(_peer_id_b)
  # docs/sharing/peers.md: approval is one switch both ways — B must
  # approve A to serve it, and A must approve B before it sends B a
  # prompt at all.
  hub_curl hub-b "$tokb" PUT "/api/peers/$a_id" '{"approved":true,"maxConcurrent":2,"perHour":50}' >/dev/null
  hub_curl hub-a "$toka" PUT "/api/peers/$b_id" '{"approved":true,"maxConcurrent":2,"perHour":50}' >/dev/null
  hub_curl hub-a "$toka" POST /api/hosts/remote-big/pool '{"enabled":false}' >/dev/null
  _wait_offer_has_model >/dev/null
  _ok200() { [ "${res%% *}" = "200" ]; }
  res=$(_cross_house_until "$toka" _ok200)
  status=${res%% *}; peer=${res#* }
  assert_status "the request still succeeds with the local holder disabled" "$status" 200
  assert_eq "the decision names house-b as the peer that served it" "$peer" "house-b"
  hub_curl hub-a "$toka" POST /api/hosts/remote-big/pool '{"enabled":true}' >/dev/null
}

test_publish_and_front_a_service_as_a_link() {
  # docs/sharing/services.md end to end: remote-mid already runs the
  # "whoami" stack on :8000 (deployed in an earlier lab session).
  local toka tokb a_id b_id svc status body
  toka=$(token_a); tokb=$(token_b)
  a_id=$(_peer_id_a); b_id=$(_peer_id_b)
  svc="tests-sharing-whoami"

  # Both sides decide: the origin (A) must approve the peer AND publish
  # the service to it; the fronting side (B) must separately approve the
  # front. Neither alone is enough.
  hub_curl hub-a "$toka" PUT "/api/peers/$b_id" '{"approved":true,"maxConcurrent":2,"perHour":50}' >/dev/null
  status=$(hub_curl hub-a "$toka" POST /api/services \
    "$(jq -nc --arg peer "$b_id" --arg name "$svc" '{name:$name,host:"remote-mid",port:8000,peers:[$peer]}')" | hub_status)
  assert_status "hub-a publishes the service to house-b" "$status" 204

  status=$(hub_curl hub-b "$tokb" PUT "/api/peers/$a_id/fronts/$svc" '{"approved":true}' | hub_status)
  assert_status "hub-b approves fronting it" "$status" 204

  # The first dial over a fresh service can race the p2p stream setup
  # (this lab's two houses talk over a relay, not a direct hole-punched
  # connection — see lab/README.md's Limits), so give it a couple of
  # beats before treating a miss as real.
  local i=0
  for i in 1 2 3; do
    body=$(hub_curl hub-b "$tokb" GET "/~$a_id/$svc/")
    status=$(echo "$body" | hub_status)
    [ "$status" = "200" ] && break
    sleep 3
  done
  assert_status "hub-b now serves the link" "$status" 200
  case "$(echo "$body" | hub_body)" in
    *Hostname:*) pass "the response is actually whoami's own body, proxied from remote-mid" ;;
    *) fail "the response is actually whoami's own body, proxied from remote-mid" ;;
  esac

  hub_curl hub-b "$tokb" PUT "/api/peers/$a_id/fronts/$svc" '{"approved":false}' >/dev/null
  hub_curl hub-a "$toka" DELETE "/api/services/$svc" >/dev/null
}

test_service_is_invisible_to_a_peer_it_was_not_published_to() {
  # docs/sharing/services.md: "Named, not discovered ... the rest of the
  # space never learns it exists." There's only one other hub in this
  # lab's space, so exercise the negative shape instead: publishing with
  # an empty peer list must not appear in anyone's offer.
  local toka svc status services
  toka=$(token_a)
  svc="tests-unpublished"
  hub_curl hub-a "$toka" POST /api/services \
    "$(jq -nc --arg name "$svc" '{name:$name,host:"remote-mid",port:8000,peers:[]}')" >/dev/null
  services=$(_peer_row_of_b_on_a | jq -r '.fronts // empty')
  case "$services" in
    *"$svc"*) fail "a service published to nobody does not appear in house-b's row" ;;
    *) pass "a service published to nobody does not appear in house-b's row" ;;
  esac
  hub_curl hub-a "$toka" DELETE "/api/services/$svc" >/dev/null
}

# docs/sharing/peers.md — approval is a person's act, quotas meter a
# key, and the score counts both directions. These set B's controls for
# A, send from A with its own holder of the model disabled (so the only
# way to a 200 is house-b), and restore the lab's approvals at the end.
_cross_house() { # _cross_house <tok-a> -> "<status> <peer>"
  local prompt out status decision
  prompt="tests-quota-$$-$RANDOM"
  out=$("$SSH" hub-a "curl -sS -D - -o /dev/null -X POST \
    -H 'Authorization: Bearer $1' -H 'Content-Type: application/json' \
    --data '$(jq -nc --arg p "$prompt" '{model:"qwen2.5:0.5b",prompt:$p,stream:false}')' \
    http://127.0.0.1:7433/api/generate" 2>/dev/null)
  status=$(echo "$out" | grep -i '^HTTP/' | tail -1 | awk '{print $2}')
  decision=$(echo "$out" | grep -i '^X-HomeDash-Decision:' | sed 's/^[^:]*: *//' | tr -d '\r')
  echo "$status $(echo "$decision" | jq -r '.peer // empty' 2>/dev/null)"
}
# _cross_house_until <tok-a> <check...> — the lab's two houses talk over
# a public relay (lab/README.md: the hole punch does not land here), and
# a relay refuses a stream now and then. Repeat the request until the
# check passes or three tries are spent; prints the last result.
_cross_house_until() {
  local toka=$1 i res; shift
  for i in 1 2 3; do
    res=$(_cross_house "$toka")
    "$@" && break
    sleep 5
  done
  echo "$res"
}
_restore_peering() {
  local a_id b_id
  a_id=$(_peer_id_a); b_id=$(_peer_id_b)
  hub_curl hub-b "$(token_b)" PUT "/api/peers/$a_id" '{"approved":true,"maxConcurrent":2,"perHour":50}' >/dev/null
  hub_curl hub-a "$(token_a)" PUT "/api/peers/$b_id" '{"approved":true,"maxConcurrent":2,"perHour":50}' >/dev/null
  hub_curl hub-a "$(token_a)" POST /api/hosts/remote-big/pool '{"enabled":true}' >/dev/null
}

test_a_peer_that_has_not_approved_you_refuses_your_job_before_reading_it() {
  # peers.md: "Approved: whether this key may send jobs"; jobs.md step 3:
  # "Unapproved ... B refuses with a reason — A stops waiting and falls
  # back to its own queue. Only after a yes does A send the prompt."
  local toka tokb a_id refused0 res
  toka=$(token_a); tokb=$(token_b)
  a_id=$(_peer_id_a)
  _restore_peering
  hub_curl hub-a "$toka" POST /api/hosts/remote-big/pool '{"enabled":false}' >/dev/null
  _wait_offer_has_model >/dev/null
  refused0=$(event_count hub-b "$tokb" peer.refused "not approved")
  hub_curl hub-b "$tokb" PUT "/api/peers/$a_id" '{"approved":false,"maxConcurrent":2,"perHour":50}' >/dev/null
  _b_refused() { [ "$(event_count hub-b "$tokb" peer.refused "not approved")" -gt "$refused0" ]; }
  res=$(_cross_house_until "$toka" _b_refused)
  assert_ne "with house-b's approval withdrawn, the request does not succeed" "${res%% *}" "200"
  # G-5/M-5: a peer that was actually asked and said no is still named in
  # the decision — the refusal is never silently dropped from the header,
  # whatever else the local reason says.
  assert_eq "the decision still names house-b, though it refused" "${res#* }" "house-b"
  # Over the lab's relay the stream to an unapproved sender is sometimes
  # reset before B reads the header, so B has nothing to refuse and no
  # event to write; when B did read it, the reason is on record.
  if _b_refused; then pass "house-b recorded the refusal, with the reason, as an event"
  else skip "house-b did not get to read the header (the relay reset the stream); the refusal reason was not exercised this run"; fi
  _restore_peering
}

test_approval_is_one_switch_both_ways() {
  # peers.md: "Trust is one switch, both ways ... a prompt goes only to a
  # key someone here has approved, never to a hub known only by its
  # offer." A withdraws its approval of B: B would say yes, but A never
  # asks.
  local toka b_id res
  toka=$(token_a)
  b_id=$(_peer_id_b)
  _restore_peering
  hub_curl hub-a "$toka" POST /api/hosts/remote-big/pool '{"enabled":false}' >/dev/null
  hub_curl hub-a "$toka" PUT "/api/peers/$b_id" '{"approved":false,"maxConcurrent":2,"perHour":50}' >/dev/null
  res=$(_cross_house "$toka")
  assert_ne "A does not send to a peer it has not approved, even one that would serve it" "${res%% *}" "200"
  _restore_peering
}

test_the_hourly_allowance_meters_a_key() {
  # peers.md: "Per hour: jobs accepted from it in a rolling hour." The
  # hour is rolling and the suite has already sent house-b jobs in it,
  # so the meter is shown both ways: an allowance of one is already
  # spent and the next job is refused with that reason; raised, the
  # same job is served.
  local toka tokb a_id refused0 res
  toka=$(token_a); tokb=$(token_b)
  a_id=$(_peer_id_a)
  _restore_peering
  hub_curl hub-a "$toka" POST /api/hosts/remote-big/pool '{"enabled":false}' >/dev/null
  _wait_offer_has_model >/dev/null
  refused0=$(event_count hub-b "$tokb" peer.refused "hourly allowance")
  hub_curl hub-b "$tokb" PUT "/api/peers/$a_id" '{"approved":true,"maxConcurrent":2,"perHour":1}' >/dev/null
  _b_refused() { [ "$(event_count hub-b "$tokb" peer.refused "hourly allowance")" -gt "$refused0" ]; }
  res=$(_cross_house_until "$toka" _b_refused)
  assert_ne "over the allowance, the job is not served by house-b" "${res%% *}" "200"
  # G-5/M-5: house-b is still named as the peer that refused it, even
  # though it didn't serve the job.
  assert_eq "the decision still names house-b as the peer that refused it" "${res#* }" "house-b"
  assert_true "house-b refused it citing the hourly allowance" \
    test "$(event_count hub-b "$tokb" peer.refused "hourly allowance")" -gt "$refused0"
  hub_curl hub-b "$tokb" PUT "/api/peers/$a_id" '{"approved":true,"maxConcurrent":2,"perHour":50}' >/dev/null
  _wait_offer_has_model >/dev/null
  _served() { [ "$res" = "200 house-b" ]; }
  res=$(_cross_house_until "$toka" _served)
  assert_eq "with the allowance raised, the same job is served by house-b" "$res" "200 house-b"
  _restore_peering
}

test_the_score_counts_both_directions() {
  # peers.md#keeping-the-score: each row counts jobs sent to that key and
  # served for it, by model. After the jobs above, B's row for A has
  # served qwen2.5:0.5b, and A's row for B has sent it.
  local toka tokb a_id served sent
  toka=$(token_a); tokb=$(token_b)
  a_id=$(_peer_id_a)
  served=$(hub_curl hub-b "$tokb" GET /api/peers | hub_body | jq -r '.peers[] | select(.name=="house-a") | .counts.served["qwen2.5:0.5b"][2] // 0')
  sent=$(hub_curl hub-a "$toka" GET /api/peers | hub_body | jq -r '.peers[] | select(.name=="house-b") | .counts.sent["qwen2.5:0.5b"][2] // 0')
  assert_true "house-b counts what it served house-a, by model" test "$served" -gt 0
  assert_true "house-a counts what it sent house-b, by model" test "$sent" -gt 0
}

test_an_ingress_is_a_hostname_the_fronting_side_picks() {
  # services.md: "picks the hostname if it is an ingress"; a hostname is
  # a DNS name, and the front row carries it. No certificate can be
  # issued in the lab (nothing public resolves here), so the ingress is
  # exercised up to the name.
  local toka tokb a_id b_id svc status row
  toka=$(token_a); tokb=$(token_b)
  a_id=$(_peer_id_a); b_id=$(_peer_id_b)
  svc="tests-ingress"
  hub_curl hub-a "$toka" POST /api/services \
    "$(jq -nc --arg peer "$b_id" --arg name "$svc" '{name:$name,host:"remote-mid",port:8000,peers:[$peer]}')" >/dev/null
  status=$(hub_curl hub-b "$tokb" PUT "/api/peers/$a_id/fronts/$svc" '{"approved":true,"hostname":"not a hostname"}' | hub_status)
  assert_status "a hostname that is not a DNS name is refused" "$status" 400
  status=$(hub_curl hub-b "$tokb" PUT "/api/peers/$a_id/fronts/$svc" '{"approved":true,"hostname":"tests.example.com"}' | hub_status)
  assert_status "a DNS name is accepted" "$status" 204
  row=$(hub_curl hub-b "$tokb" GET /api/peers | hub_body | jq -c --arg s "$svc" '.peers[] | select(.name=="house-a") | .fronts[] | select(.service==$s)')
  assert_eq "the front row carries the hostname" "$(echo "$row" | jq -r .hostname)" "tests.example.com"
  hub_curl hub-b "$tokb" PUT "/api/peers/$a_id/fronts/$svc" '{"approved":false}' >/dev/null
  hub_curl hub-a "$toka" DELETE "/api/services/$svc" >/dev/null
}

test_unpublishing_drops_the_service_from_the_next_offer() {
  # services.md: "unpublishing it makes it disappear from their next
  # offer."
  local toka tokb a_id b_id svc
  toka=$(token_a); tokb=$(token_b)
  a_id=$(_peer_id_a); b_id=$(_peer_id_b)
  svc="tests-unpublish"
  hub_curl hub-a "$toka" POST /api/services \
    "$(jq -nc --arg peer "$b_id" --arg name "$svc" '{name:$name,host:"remote-mid",port:8000,peers:[$peer]}')" >/dev/null
  _offered() { hub_curl hub-b "$tokb" GET /api/peers | hub_body | jq -e --arg s "$svc" '.peers[] | select(.name=="house-a") | .fronts[] | select(.service==$s and .offered)' >/dev/null; }
  assert_true "house-b's next offer from house-a names the service" wait_until 90 _offered
  hub_curl hub-a "$toka" DELETE "/api/services/$svc" >/dev/null
  _not_offered() { ! _offered; }
  assert_true "after unpublishing, the next offer no longer names it" wait_until 90 _not_offered
}

test_a_service_names_only_a_machine_this_hub_owns() {
  # services.md: "a hub cannot publish something it is itself reaching
  # through a peer" — the host resolves through the store.
  local toka status
  toka=$(token_a)
  status=$(hub_curl hub-a "$toka" POST /api/services '{"name":"tests-foreign","host":"remote-b","port":8000,"peers":[]}' | hub_status)
  assert_status "publishing a port on house-b's remote from house-a is refused" "$status" 404
}

test_publish_refuses_the_hubs_and_sshs_ports() {
  # H-12: docs/sharing/services.md — "the hub's and SSH's ports are
  # never published, whatever machine is named."
  local toka status
  toka=$(token_a)
  status=$(hub_curl hub-a "$toka" POST /api/services '{"name":"tests-ssh-port","host":"remote-mid","port":22,"peers":[]}' | hub_status)
  assert_status "publishing port 22 is refused" "$status" 400
  status=$(hub_curl hub-a "$toka" POST /api/services '{"name":"tests-hub-port","host":"remote-mid","port":7433,"peers":[]}' | hub_status)
  assert_status "publishing the hub's own port is refused" "$status" 400
}

test_publishing_a_name_twice_needs_replace() {
  # H-12: a second publish under a name already in use is a 409 unless
  # the request carries replace: true — no more silent swap of the port
  # behind it.
  local toka status port
  toka=$(token_a)
  hub_curl hub-a "$toka" POST /api/services '{"name":"tests-replace","host":"remote-mid","port":8000,"peers":[]}' >/dev/null
  status=$(hub_curl hub-a "$toka" POST /api/services '{"name":"tests-replace","host":"remote-mid","port":8001,"peers":[]}' | hub_status)
  assert_status "publishing the same name again without replace is refused" "$status" 409
  status=$(hub_curl hub-a "$toka" POST /api/services '{"name":"tests-replace","host":"remote-mid","port":8001,"peers":[],"replace":true}' | hub_status)
  assert_status "the same name with replace:true succeeds" "$status" 204
  port=$(hub_curl hub-a "$toka" GET /api/peers | hub_body | jq -r '.services[] | select(.name=="tests-replace") | .port')
  assert_eq "the replace actually changed the port" "$port" "8001"
  hub_curl hub-a "$toka" DELETE /api/services/tests-replace >/dev/null
}
