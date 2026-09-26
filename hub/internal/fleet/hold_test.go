package fleet

import (
	"encoding/base64"
	"strings"
	"testing"
)

// The stop-post line goes through systemd's own parser: no $ or % may
// reach it, and what it decodes to must be hold.sh with the hub's key.
func TestHoldStopPost(t *testing.T) {
	f := &Fleet{PubKey: "ssh-ed25519 AAAAC3Nza hub@homedash\n"}
	line := f.HoldStopPost()
	if strings.ContainsAny(line, "$%'") {
		t.Fatalf("systemd would expand part of %q", line)
	}
	fields := strings.Fields(line)
	if len(fields) < 4 || fields[0] != "/bin/sh" || fields[1] != "-c" {
		t.Fatalf("unexpected shape: %q", line)
	}
	raw, err := base64.StdEncoding.DecodeString(fields[3])
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.HasPrefix(body, "PUBKEY='ssh-ed25519 AAAAC3Nza hub@homedash'\n") || !strings.Contains(body, "sudoers") {
		t.Fatalf("decoded body is not hold.sh with the key: %.120q", body)
	}
	if !strings.HasSuffix(line, `> /etc/homedash/hold-restored 2>&1"`) {
		t.Errorf("result not left for CheckHold: %q", line)
	}
}
