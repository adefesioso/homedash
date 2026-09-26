package store

import (
	"context"
	"testing"
)

// Hub replies land with their file offset in one commit; served rows sum
// per server, caller and model.
func TestHubAndServedUsage(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.RecordHubUsage(ctx, "/s/a.jsonl", 42, []Usage{{At: since(1), Model: "m", Input: 3, Output: 4, Cost: 0.1}}); err != nil {
		t.Fatal(err)
	}
	if off, err := st.HubUsageOffset(ctx, "/s/a.jsonl"); err != nil || off != 42 {
		t.Fatalf("offset = %d, %v", off, err)
	}
	hub, err := st.HubUsage(ctx, 24)
	if err != nil || len(hub) != 1 || hub[0].Input != 3 || hub[0].Calls != 1 {
		t.Fatalf("hub = %+v, %v", hub, err)
	}
	for i := 0; i < 2; i++ {
		if err := st.RecordServed(ctx, Served{Server: "big", Caller: "hub", Model: "q", Input: 10, Output: 5}); err != nil {
			t.Fatal(err)
		}
	}
	vs, err := st.ServedUsage(ctx, 24)
	if err != nil || len(vs) != 1 || vs[0].Input != 20 || vs[0].Calls != 2 {
		t.Fatalf("served = %+v, %v", vs, err)
	}
	if err := st.RollupUsage(ctx); err != nil {
		t.Fatal(err)
	}
}
