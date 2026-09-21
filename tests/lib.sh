# Shared helpers for the acceptance suite. Every test_*.sh does
# `. "$(dirname "$0")/lib.sh"`.
#
# Tests run commands *on* hub-a / hub-b themselves (over the lab's SSH jump
# through Proxmox), never from this machine directly — the houses are not
# routed to on purpose, which is one of the claims under test.

TESTS_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
LAB_DIR=$(cd "$TESTS_DIR/../lab" && pwd)
SSH="$LAB_DIR/ssh.sh"

PASS=0
FAIL=0
SKIP=0
CURRENT_FILE=""

pass() { PASS=$((PASS+1)); printf '  \033[1;32mok\033[0m   %s\n' "$*"; }
fail() { FAIL=$((FAIL+1)); printf '  \033[1;31mFAIL\033[0m %s\n' "$*"; }
skip() { SKIP=$((SKIP+1)); printf '  \033[1;33mskip\033[0m %s\n' "$*"; }

# assert <description> <actual> <expected> — string equality.
assert_eq() {
  local desc=$1 actual=$2 expected=$3
  if [ "$actual" = "$expected" ]; then pass "$desc"
  else fail "$desc (got [$actual], want [$expected])"; fi
}

assert_ne() {
  local desc=$1 actual=$2 unexpected=$3
  if [ "$actual" != "$unexpected" ]; then pass "$desc"
  else fail "$desc (got [$actual], did not want [$unexpected])"; fi
}

# assert_status <description> <curl-status-var-value> <want>
assert_status() {
  local desc=$1 got=$2 want=$3
  if [ "$got" = "$want" ]; then pass "$desc"
  else fail "$desc (HTTP $got, want $want)"; fi
}

assert_true() {
  local desc=$1; shift
  if "$@"; then pass "$desc"; else fail "$desc"; fi
}

# hub_token <hub> — mints an admin API token named "tests-suite" on the
# named hub (hub-a|hub-b), run from a shell on the hub as the docs say a
# script should: `sudo -u homedash homedash token NAME`. A fixed name (not
# one per PID) keeps repeated runs from leaving a token behind on every
# invocation; run.sh deletes it via the API when the suite finishes.
hub_token() {
  local hub=$1
  "$SSH" "$hub" "sudo -u homedash homedash token tests-suite" 2>/dev/null
}

# hub_curl <hub> <token> <method> <path> [json-body]
# Runs curl *on* the hub against its own loopback API and prints
# "<http_status>\n<body>".
hub_curl() {
  local hub=$1 token=$2 method=$3 path=$4 body=${5:-}
  if [ -n "$body" ]; then
    "$SSH" "$hub" "curl -sS -o /tmp/hd-resp.$$ -w '%{http_code}' -X $method \
      -H 'Authorization: Bearer $token' -H 'Content-Type: application/json' \
      --data-binary @- 'http://127.0.0.1:7433$path'; echo; cat /tmp/hd-resp.$$; rm -f /tmp/hd-resp.$$" <<<"$body"
  else
    "$SSH" "$hub" "curl -sS -o /tmp/hd-resp.$$ -w '%{http_code}' -X $method \
      -H 'Authorization: Bearer $token' 'http://127.0.0.1:7433$path'; echo; cat /tmp/hd-resp.$$; rm -f /tmp/hd-resp.$$"
  fi
}

# hub_curl_local <hub> <token> <path> <json-body> — like hub_curl POST but
# sets X-HomeDash-Local-Only, which internal/pool/pool.go's Serve uses to
# skip the peer fallback (the same header a job arriving from a peer
# carries, so it can never itself fall through to a peer).
hub_curl_local() {
  local hub=$1 token=$2 path=$3 body=$4
  "$SSH" "$hub" "curl -sS -o /tmp/hd-resp.$$ -w '%{http_code}' -X POST \
    -H 'Authorization: Bearer $token' -H 'Content-Type: application/json' -H 'X-HomeDash-Local-Only: 1' \
    --data-binary @- 'http://127.0.0.1:7433$path'; echo; cat /tmp/hd-resp.$$; rm -f /tmp/hd-resp.$$" <<<"$body"
}

# hub_status / hub_body split hub_curl's combined output.
hub_status() { head -1; }
hub_body() { tail -n +2; }

# agent_run <hub> <token> <host> <command> — runs <command> on <host> as
# the job account, homedash-agent, through the executor with the door
# forwards up, exactly the way a job's own shell sees the machine
# (docs/running/safety.md, docs/pooling/agents/README.md). Prints
# hub_curl's combined output; stdout ends with "exit=<rc>".
agent_run() {
  local hub=$1 tok=$2 host=$3 cmd=$4
  hub_curl "$hub" "$tok" POST "/api/hosts/$host/run" \
    "$(jq -nc --arg c "sudo -n -u homedash-agent sh -c $(printf '%q' "$cmd") 2>&1; echo exit=\$?" '{command:$c,timeoutSeconds:60,forwards:true}')"
}

# hub_events <hub> <token> <kind> — the messages of recent events of one
# kind, newest first, one per line (docs/pooling/notifications.md:
# events record transitions).
hub_events() {
  hub_curl "$1" "$2" GET /api/events | hub_body | jq -r --arg k "$3" '.[] | select(.kind==$k) | .message'
}

# event_count <hub> <token> <kind> <substring> — how many recent events
# of that kind mention the substring. Compare before and after an action
# rather than looking for a message, since earlier runs leave the same
# messages behind.
event_count() {
  hub_events "$1" "$2" "$3" | grep -cF -- "$4" || true
}

# wait_until <seconds> <command...> — polls until the command exits 0 or
# the time is up; exit status is the last command's.
wait_until() {
  local secs=$1; shift
  local i=0
  while [ $i -lt "$secs" ]; do
    "$@" && return 0
    sleep 2; i=$((i+2))
  done
  "$@"
}

# token_a / token_b — cached admin tokens, minted once per run.
token_a() { [ -n "${HUB_A_TOK:-}" ] || HUB_A_TOK=$(hub_token hub-a); echo "$HUB_A_TOK"; }
token_b() { [ -n "${HUB_B_TOK:-}" ] || HUB_B_TOK=$(hub_token hub-b); echo "$HUB_B_TOK"; }

# run_test_file <path> — sources it and calls every function starting
# with test_, in the order they were defined.
run_test_file() {
  local f=$1
  CURRENT_FILE=$(basename "$f")
  echo ""
  echo "== $CURRENT_FILE =="
  # shellcheck source=/dev/null
  . "$f"
  # declare -F sorts by name; the file's order is the order the tests
  # depend on (a stack is deployed before it is stopped), so read the
  # definitions from the file.
  local fn
  for fn in $(grep -oE '^test_[A-Za-z0-9_]+\(\)' "$f" | tr -d '()'); do
    "$fn"
    unset -f "$fn"
  done
}
