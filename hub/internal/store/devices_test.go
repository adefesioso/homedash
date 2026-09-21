package store

import (
	"context"
	"encoding/json"
	"testing"
)

// Devices and sightings: one row per (kind, addr) however many remotes
// see it, your name outliving the forgetting, and the scan table.
func TestDevices(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	nas, _ := st.AddHost(ctx, &Host{Name: "nas", Addr: "10.0.0.2", Port: 22, User: "homedash", HostKey: "k"})
	pi, _ := st.AddHost(ctx, &Host{Name: "pi", Addr: "10.0.0.3", Port: 22, User: "homedash", HostKey: "k"})

	id, created, err := st.Sight(ctx, "lan", "AA:BB:CC:00:00:01", nas, json.RawMessage(`{"ip":"10.0.0.9"}`))
	if err != nil || !created {
		t.Fatalf("first sighting: %v %v", created, err)
	}
	id2, created, err := st.Sight(ctx, "lan", "aa:bb:cc:00:00:01", pi, json.RawMessage(`{"ip":"10.0.0.9","hostname":"tv"}`))
	if err != nil || created || id2 != id {
		t.Fatalf("second sighting: id %d/%d created %v err %v", id, id2, created, err)
	}
	if _, _, err := st.Sight(ctx, "bt", "aa:bb:cc:00:00:01", pi, nil); err != nil {
		t.Fatal(err)
	}
	ds, err := st.Devices(ctx)
	if err != nil || len(ds) != 2 {
		t.Fatalf("devices: %d %v", len(ds), err)
	}
	d, err := st.Device(ctx, "lan:aa:bb:cc:00:00:01")
	if err != nil || d.ID != id || len(d.Sightings) != 2 {
		t.Fatalf("device by ref: %+v %v", d, err)
	}
	if err := st.SetGuess(ctx, id, "tv", "tv", "LG"); err != nil {
		t.Fatal(err)
	}
	if err := st.NameDevice(ctx, id, "Living room TV", "tv"); err != nil {
		t.Fatal(err)
	}
	d, _ = st.Device(ctx, "lan:aa:bb:cc:00:00:01")
	if d.Name != "Living room TV" || d.GuessName != "tv" || d.Vendor != "LG" {
		t.Fatalf("named: %+v", d)
	}

	// Forgetting: an unnamed device unseen for the retention goes; a named one stays.
	if _, err := st.DB.ExecContext(ctx, `UPDATE devices SET last_seen = '2000-01-01T00:00:00.000Z'`); err != nil {
		t.Fatal(err)
	}
	if err := st.ForgetStale(ctx, 30); err != nil {
		t.Fatal(err)
	}
	ds, _ = st.Devices(ctx)
	if len(ds) != 1 || ds[0].Name != "Living room TV" {
		t.Fatalf("after forgetting: %+v", ds)
	}
	if err := st.ForgetDevice(ctx, id); err != nil {
		t.Fatal(err)
	}
	if ds, _ = st.Devices(ctx); len(ds) != 0 {
		t.Fatalf("forget: %+v", ds)
	}

	if err := st.SetHostScan(ctx, nas, true, false, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := st.SetHostScan(ctx, nas, true, true, true, ""); err != nil {
		t.Fatal(err)
	}
	sc, err := st.HostScans(ctx)
	if err != nil || len(sc) != 1 || !sc[0].WiFi || sc[0].HostName != "nas" {
		t.Fatalf("scans: %+v %v", sc, err)
	}
}
