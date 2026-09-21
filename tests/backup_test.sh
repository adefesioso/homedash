# docs/running/README.md#where-the-state-lives — state survives the hub
# by export: one passphrase-encrypted file, restored into a hub.
#
# The round trip runs on hub-a against its own export: a setting is
# changed after the export, the export is restored, and the change is
# gone once the hub is back. Everything between export and restore on
# hub-a is lost by design — this suite keeps that window to seconds.

_PASS=tests-passphrase
_FILE=/tmp/hd-tests-export.age
_status() { hub_curl hub-a "$(token_a)" GET /api/backup | hub_body; }
_hub_up() { "$SSH" hub-a "curl -sf -o /dev/null http://127.0.0.1:7433/api/health" 2>/dev/null; }

test_export_needs_a_passphrase() {
  local out
  out=$(hub_curl hub-a "$(token_a)" POST /api/backup/export '{}')
  assert_status "an export without a passphrase is refused" "$(echo "$out" | hub_status)" 400
  case "$(echo "$out" | hub_body)" in
    *passphrase*) pass "the refusal names the passphrase" ;;
    *) fail "the refusal names the passphrase (got: $(echo "$out" | hub_body))" ;;
  esac
}

test_export_downloads_one_encrypted_file() {
  local tok status head last
  tok=$(token_a)
  status=$("$SSH" hub-a "curl -sS -o $_FILE -w '%{http_code}' -X POST -H 'Authorization: Bearer $tok' -H 'Content-Type: application/json' -d '{\"passphrase\":\"$_PASS\"}' http://127.0.0.1:7433/api/backup/export" 2>/dev/null)
  assert_status "POST /api/backup/export succeeds" "$status" 200
  # age's binary format opens with its own header line; a plain tar
  # would start with a file name.
  head=$("$SSH" hub-a "head -c 21 $_FILE" 2>/dev/null)
  assert_eq "the file is age-encrypted, not a plain tar" "$head" "age-encryption.org/v1"
  last=$(_status | jq -r '.last // empty')
  assert_true "the status records when the last export was made" test -n "$last"
}

test_restore_refuses_a_wrong_passphrase_without_damage() {
  local tok out
  tok=$(token_a)
  out=$("$SSH" hub-a "curl -sS -o /tmp/hd-resp.$$ -w '%{http_code}' -X POST -H 'Authorization: Bearer $tok' -F file=@$_FILE -F passphrase=wrong http://127.0.0.1:7433/api/backup/restore; echo; cat /tmp/hd-resp.$$; rm -f /tmp/hd-resp.$$" 2>/dev/null)
  assert_status "a restore with the wrong passphrase is refused" "$(echo "$out" | hub_status)" 400
  case "$(echo "$out" | hub_body)" in
    *passphrase*) pass "the refusal says it is the passphrase" ;;
    *) fail "the refusal says it is the passphrase (got: $(echo "$out" | hub_body))" ;;
  esac
  assert_true "the hub is still up: nothing was staged" _hub_up
  assert_eq "no staging directory is left behind" "$("$SSH" hub-a "sudo test -d /var/lib/homedash/restore.pending && echo yes || echo no" 2>/dev/null)" "no"
}

test_restore_puts_the_export_back_and_the_hub_restarts() {
  local tok status name before
  tok=$(token_a)
  before=$(hub_curl hub-a "$tok" GET /api/settings | hub_body | jq -r '."hub.lan_addr" // empty')
  hub_curl hub-a "$tok" PUT /api/settings '{"hub.lan_addr":"203.0.113.9"}' >/dev/null
  status=$("$SSH" hub-a "curl -sS -o /dev/null -w '%{http_code}' -X POST -H 'Authorization: Bearer $tok' -F file=@$_FILE -F passphrase=$_PASS http://127.0.0.1:7433/api/backup/restore" 2>/dev/null)
  assert_status "POST /api/backup/restore is accepted" "$status" 202
  sleep 3
  assert_true "the hub comes back within a minute" wait_until 60 _hub_up
  # The restored database predates the token this run minted; mint again.
  HUB_A_TOK=""
  tok=$(token_a)
  name=$(hub_curl hub-a "$tok" GET /api/settings | hub_body | jq -r '."hub.lan_addr" // empty')
  assert_eq "a setting changed after the export is gone" "$name" "$before"
  assert_eq "the restore is recorded as an event" "$(event_count hub-a "$tok" backup.restored "restored from an export")" "1"
  "$SSH" hub-a "rm -f $_FILE" >/dev/null 2>&1
}
