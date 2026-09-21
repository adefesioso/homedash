package store

import (
	"context"
	"testing"
)

// The rollup reads only hours not yet rolled, and never rewrites one.
func TestRollupBounded(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	id, err := st.AddHost(ctx, &Host{Name: "a", Addr: "1.2.3.4", Port: 22, User: "u", HostKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	// two rows three hours ago, two rows two hours ago
	for _, h := range []string{"-3 hours", "-2 hours"} {
		for i := 0; i < 2; i++ {
			if _, err := st.DB.Exec(`INSERT INTO metrics(host_id, at, mem_used, mem_total, load1, gpu_busy, mounts) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ','now',?), ?, 100, 1.0, 0, '{"/":10}')`, id, h, 10*(i+1)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := st.RollupMetrics(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	st.RO.QueryRow(`SELECT COUNT(*) FROM metrics_hourly`).Scan(&n)
	if n != 2 {
		t.Fatalf("hourly rows = %d, want 2", n)
	}
	var mu int64
	st.RO.QueryRow(`SELECT mem_used FROM metrics_hourly ORDER BY hour LIMIT 1`).Scan(&mu)
	if mu != 15 {
		t.Fatalf("avg = %d, want 15", mu)
	}
	// a second rollup reads nothing new and changes nothing
	if _, err := st.DB.Exec(`UPDATE metrics_hourly SET mem_used = 99`); err != nil {
		t.Fatal(err)
	}
	if err := st.RollupMetrics(ctx); err != nil {
		t.Fatal(err)
	}
	st.RO.QueryRow(`SELECT mem_used FROM metrics_hourly ORDER BY hour LIMIT 1`).Scan(&mu)
	if mu != 99 {
		t.Fatalf("second rollup rewrote a rolled hour: %d", mu)
	}
	// a new complete hour (one hour ago) is picked up
	if _, err := st.DB.Exec(`INSERT INTO metrics(host_id, at, mem_used, mem_total, load1, gpu_busy, mounts) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ','now','-1 hours'), 40, 100, 1.0, 0, '{"/":10}')`, id); err != nil {
		t.Fatal(err)
	}
	if err := st.RollupMetrics(ctx); err != nil {
		t.Fatal(err)
	}
	st.RO.QueryRow(`SELECT COUNT(*) FROM metrics_hourly`).Scan(&n)
	if n != 3 {
		t.Fatalf("hourly rows = %d, want 3", n)
	}
	m, _ := st.HostMetrics(ctx, id, 48)
	if len(m) < 3 {
		t.Fatalf("series = %d", len(m))
	}
}
