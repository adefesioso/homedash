package agent

import (
	"strings"
	"testing"
)

func TestParseChanges(t *testing.T) {
	report := "## Report\nInstalled mergerfs.\n\n```homedash-changes\n{\"packages\": [\"apt-get install -y mergerfs\"],\n \"catalog\": [{\"entry\": \"jellyfin\", \"note\": \"needs /dev/dri\"}]}\n```\n"
	got, bad := parseChanges(report)
	if bad != "" || !strings.Contains(got, `"packages":["apt-get install -y mergerfs"]`) || !strings.Contains(got, `"entry":"jellyfin"`) {
		t.Fatalf("parseChanges = %q, %q", got, bad)
	}
	if got, bad := parseChanges("## Report\nnothing changed"); got != "" || bad != "" {
		t.Errorf("no block: %q, %q", got, bad)
	}
	if _, bad := parseChanges("```homedash-changes\n[1,2]\n```"); bad == "" {
		t.Error("an array was accepted")
	}
	if _, bad := parseChanges("```homedash-changes\n{\"a\":1}"); bad == "" {
		t.Error("an unclosed block was accepted")
	}
	// The last block wins: a correction round's report restates it.
	got, _ = parseChanges("```homedash-changes\n{\"a\":1}\n```\nthen\n```homedash-changes\n{\"b\":2}\n```")
	if got != `{"b":2}` {
		t.Errorf("last block: %q", got)
	}
}
