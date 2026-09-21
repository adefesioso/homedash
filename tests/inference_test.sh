# docs/pooling/inference.md — one endpoint, Ollama's own shape, a request
# names a model and matches it exactly, a machine can be taken out of the
# pool without removing anything, and one placement decision per
# conversation.
#
# Every request below uses a unique first-turn prompt: a conversation is
# identified by its message history (docs: "the hub recognizes a turn
# whose messages begin with a history it has already placed"), so reusing
# a prompt string across tests would silently pin every one of them to
# whichever machine answered it first.

_pool_set() { hub_curl hub-a "$1" POST "/api/hosts/$2/pool" "{\"enabled\":$3}" >/dev/null; }
_probe() { echo "tests-probe-$$-$RANDOM"; }

test_router_speaks_ollamas_shape() {
  local tok status
  tok=$(token_a)
  status=$(hub_curl hub-a "$tok" GET /api/tags | hub_status)
  assert_status "GET /api/tags (Ollama's own endpoint) answers" "$status" 200
}

test_disabled_machine_is_excluded_even_if_it_holds_the_model() {
  # remote-big is the only local machine holding qwen2.5:0.5b — but this
  # lab's two houses are peered (docs/sharing/jobs.md), so a plain request
  # would correctly fall through to hub-b's pool and still succeed. Force
  # local-only to isolate the claim under test: pooling.md's "a card can
  # also take its machine out of the pool without removing anything."
  local tok status body prompt
  tok=$(token_a)
  _pool_set "$tok" remote-big false
  prompt=$(_probe)
  body=$(hub_curl_local hub-a "$tok" /api/generate \
    "$(jq -nc --arg p "$prompt" '{model:"qwen2.5:0.5b",prompt:$p,stream:false}')")
  status=$(echo "$body" | hub_status)
  assert_status "a fresh conversation for a model only a disabled machine holds is refused, local-only" "$status" 404
}

test_enabling_the_machine_lets_the_request_place() {
  local tok body status prompt
  tok=$(token_a)
  _pool_set "$tok" remote-big true
  prompt=$(_probe)
  body=$(hub_curl hub-a "$tok" POST /api/generate \
    "$(jq -nc --arg p "$prompt" '{model:"qwen2.5:0.5b",prompt:$p,stream:false}')")
  status=$(echo "$body" | hub_status)
  assert_status "re-enabling remote-big lets a fresh request place" "$status" 200
}

test_request_names_a_model_and_matches_exactly() {
  local tok status prompt
  tok=$(token_a)
  prompt=$(_probe)
  status=$(hub_curl hub-a "$tok" POST /api/generate \
    "$(jq -nc --arg p "$prompt" '{model:"qwen2.5:0.5b-does-not-exist",prompt:$p,stream:false}')" | hub_status)
  assert_status "a model name that isn't an exact match is not substituted" "$status" 404
}

test_decision_is_visible() {
  # docs/pooling/inference.md: "Every response the router returns names
  # where it was placed and the one reason it went there" — carried as
  # the X-HomeDash-Decision header (internal/pool/pool.go).
  local tok decision host reason prompt
  tok=$(token_a)
  prompt=$(_probe)
  decision=$("$SSH" hub-a "curl -sS -D - -o /dev/null -X POST \
    -H 'Authorization: Bearer $tok' -H 'Content-Type: application/json' \
    --data '$(jq -nc --arg p "$prompt" '{model:"qwen2.5:0.5b",prompt:$p,stream:false}')' \
    http://127.0.0.1:7433/api/generate" 2>/dev/null | grep -i '^X-HomeDash-Decision:' | sed 's/^[^:]*: *//' | tr -d '\r')
  assert_true "the response carries an X-HomeDash-Decision header" test -n "$decision"
  host=$(echo "$decision" | jq -r '.host // empty' 2>/dev/null)
  reason=$(echo "$decision" | jq -r '.reason // empty' 2>/dev/null)
  assert_eq "the decision names remote-big as where it was placed" "$host" "remote-big"
  assert_true "the decision carries a reason" test -n "$reason"
}

test_conversation_stays_home_across_turns() {
  # docs/pooling/inference.md: "every turn after it goes to the same
  # place ... for as long as that place is still there and still says
  # yes." /api/generate is always a single-message "conversation" (no
  # prior turn to key off), so this needs /api/chat with a growing
  # messages array — pool.go hashes the *exact bytes* of each message, so
  # turn one's user message must be byte-identical in both calls.
  local tok prompt m0 body1 body2 decision2 reason2
  tok=$(token_a)
  prompt=$(_probe)
  m0=$(jq -nc --arg p "$prompt" '{role:"user",content:$p}')
  body1=$(jq -nc --argjson m0 "$m0" '{model:"qwen2.5:0.5b",messages:[$m0],stream:false}')
  "$SSH" hub-a "curl -sS -o /dev/null -X POST \
    -H 'Authorization: Bearer $tok' -H 'Content-Type: application/json' \
    --data '$body1' http://127.0.0.1:7433/api/chat" >/dev/null 2>&1

  body2=$(jq -nc --argjson m0 "$m0" '{model:"qwen2.5:0.5b",messages:[$m0,{role:"assistant",content:"ok"}],stream:false}')
  decision2=$("$SSH" hub-a "curl -sS -D - -o /dev/null -X POST \
    -H 'Authorization: Bearer $tok' -H 'Content-Type: application/json' \
    --data '$body2' http://127.0.0.1:7433/api/chat" 2>/dev/null \
    | grep -i '^X-HomeDash-Decision:' | sed 's/^[^:]*: *//' | tr -d '\r')
  reason2=$(echo "$decision2" | jq -r '.reason // empty' 2>/dev/null)
  assert_eq "the second turn's decision cites the conversation's home, not a fresh pick" \
    "$reason2" "this was the conversation's home from turn one"
}

test_the_models_grid_shows_every_machine_and_what_it_holds() {
  # docs/pooling/inference.md#getting-the-models-onto-the-machines: every
  # Ollama machine down one side, every model any of them holds across.
  local tok grid row
  tok=$(token_a)
  grid=$(hub_curl hub-a "$tok" GET /api/models | hub_body)
  row=$(echo "$grid" | jq -c '.[] | select(.host=="remote-big")')
  assert_true "remote-big is a column in the grid" test -n "$row"
  assert_eq "it is enabled, online and holds qwen2.5:0.5b" \
    "$(echo "$row" | jq -r '[.enabled, .online, ([.models[].name] | index("qwen2.5:0.5b") != null)] | map(tostring) | join(" ")')" "true true true"
  assert_true "the column totals against its free space" test "$(echo "$row" | jq -r .free)" -gt 0
}

test_what_fits_asks_llmfit_on_the_machine() {
  # "Every column has a What fits action: the hub asks llmfit on that
  # machine ... and lists the top fits ... and the speed it expects."
  local tok out status n
  tok=$(token_a)
  out=$(hub_curl hub-a "$tok" GET "/api/hosts/remote-big/fit?n=3")
  status=$(echo "$out" | hub_status)
  assert_status "GET /api/hosts/remote-big/fit answers" "$status" 200
  n=$(echo "$out" | hub_body | jq 'if type=="array" then length else (.models // .fits // []) | length end')
  assert_true "it lists at least one fit" test "$n" -gt 0
}

test_streaming_requests_stream() {
  # "Streaming requests stream." — a stream:true generate is many JSON
  # lines, the last of which says done.
  local tok prompt out lines last
  tok=$(token_a)
  prompt=$(_probe)
  out=$("$SSH" hub-a "curl -sS -N -X POST -H 'Authorization: Bearer $tok' -H 'Content-Type: application/json' \
    --data '$(jq -nc --arg p "$prompt" '{model:"qwen2.5:0.5b",prompt:$p,stream:true,options:{num_predict:8}}')' \
    http://127.0.0.1:7433/api/generate" 2>/dev/null)
  lines=$(echo "$out" | grep -c '"response"')
  assert_true "the answer arrived as more than one chunk" test "$lines" -gt 1
  last=$(echo "$out" | grep '"done"' | tail -1 | jq -r .done 2>/dev/null)
  assert_eq "the last chunk says done" "$last" "true"
}

test_pulling_a_bogus_model_says_why() {
  # M-1: Ollama answers 200 for a pull and puts the reason inside the
  # stream rather than the status ("pull model manifest: file does not
  # exist"), so the proxy either relays a non-2xx status or the body
  # carries an "error" line — this asserts whichever it does.
  local tok body status errline
  tok=$(token_a)
  body=$(hub_curl hub-a "$tok" POST /api/models/pull "$(jq -nc '{host:"remote-big",model:"no-such-model-xyz"}')")
  status=$(echo "$body" | hub_status)
  if [ "$status" != "200" ]; then
    assert_true "a bogus model's pull answers a non-2xx status" test "$status" -ge 300
  else
    errline=$(echo "$body" | hub_body | grep -o '"error"[^}]*' | head -1)
    assert_true "a bogus model's pull carries an \"error\" line in the stream" test -n "$errline"
  fi
}

test_pull_a_model_to_a_machine_and_remove_it_to_get_the_disk_back() {
  # "Pull a model to one machine or to several at once and the cells
  # fill in as it lands; remove one to get the disk back." all-minilm is
  # the smallest thing in the library (~45 MB), so this stays quick.
  local tok model status
  tok=$(token_a)
  model="all-minilm:22m"
  status=$("$SSH" hub-a "curl -sS -o /dev/null -w '%{http_code}' -m 600 -X POST -H 'Authorization: Bearer $tok' -H 'Content-Type: application/json' \
    --data '$(jq -nc --arg m "$model" '{host:"remote-big",model:$m}')' http://127.0.0.1:7433/api/models/pull" 2>/dev/null)
  assert_status "pulling $model to remote-big is accepted" "$status" 200
  _holds() { hub_curl hub-a "$tok" GET /api/models | hub_body | jq -e --arg m "$model" '.[] | select(.host=="remote-big") | .models[] | select(.name==$m)' >/dev/null; }
  assert_true "the grid's cell fills in once it lands" wait_until 60 _holds
  status=$(hub_curl hub-a "$tok" POST /api/models/delete "$(jq -nc --arg m "$model" '{host:"remote-big",model:$m}')" | hub_status)
  case "$status" in 200|204) pass "removing it is accepted" ;; *) fail "removing it is accepted (HTTP $status)" ;; esac
  _gone() { ! _holds; }
  assert_true "the cell empties again" wait_until 30 _gone
}
