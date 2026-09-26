package agent

import "testing"

// Only assistant replies with usage count; a half-written line is left
// for the next scan by the caller, never parsed here.
func TestParseSessionUsage(t *testing.T) {
	b := []byte(`{"type":"session","id":"x","timestamp":"2026-09-07T20:35:27.203Z"}
{"type":"message","timestamp":"2026-09-07T20:36:23.710Z","message":{"role":"user","content":[]}}
{"type":"message","timestamp":"2026-09-07T20:36:24.000Z","message":{"role":"assistant","provider":"openrouter","model":"z-ai/glm","usage":{"input":10,"output":5,"cacheRead":2,"cacheWrite":0,"cost":{"total":0.5}}}}
not json "usage"`)
	us := parseSessionUsage(b)
	if len(us) != 1 {
		t.Fatalf("got %d replies, want 1", len(us))
	}
	u := us[0]
	if u.Model != "openrouter/z-ai/glm" || u.Input != 10 || u.Output != 5 || u.CacheRead != 2 || u.Cost != 0.5 || u.At != "2026-09-07T20:36:24.000Z" {
		t.Fatalf("got %+v", u)
	}
}
