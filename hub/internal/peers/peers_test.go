package peers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/adefesioso/homedash/hub/internal/fleet"
	"github.com/adefesioso/homedash/hub/internal/pool"
	"github.com/adefesioso/homedash/hub/internal/store"
)

// node is one hub on the loopback: a real store, a real host, no DHT.
func node(t *testing.T) (*Peers, host.Host) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	notify := func(string, string, string) {}
	fl := &fleet.Fleet{Store: st, Notify: notify, Log: log}
	pl := pool.New(st, fl, notify, log)
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"), libp2p.DisableRelay())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	// The router answers any prompt with its path, so a test can see what
	// reached it.
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_, _ = w.Write([]byte("served " + r.URL.Path + " " + string(b)))
	})
	p := &Peers{Store: st, Pool: pl, Fleet: fl, StateDir: t.TempDir(), Notify: notify, Log: log, Router: router}
	p.attach(ctx, h, nil, func() {}, netConfig{Space: "test"})
	return p, h
}

func connect(t *testing.T, a, b host.Host) {
	t.Helper()
	if err := a.Connect(context.Background(), peer.AddrInfo{ID: b.ID(), Addrs: b.Addrs()}); err != nil {
		t.Fatal(err)
	}
}

func (p *Peers) know(id peer.ID) {
	p.mu.Lock()
	p.known[id] = true
	p.mu.Unlock()
}

func TestOfferFromAStrangerIsResetUnread(t *testing.T) {
	a, ha := node(t)
	_, hb := node(t)
	connect(t, hb, ha)
	// b greets a; a never found b at the rendezvous.
	s, err := hb.NewStream(context.Background(), ha.ID(), protoOffer)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.NewEncoder(s).Encode(Offer{Name: "stranger", Models: []string{"m"}})
	_ = s.SetReadDeadline(time.Now().Add(5 * time.Second))
	var theirs Offer
	if err := json.NewDecoder(s).Decode(&theirs); err == nil {
		t.Fatal("a stranger got an offer back")
	}
	if rows, _ := a.Store.Peers(context.Background()); len(rows) != 0 {
		t.Fatalf("a stranger made it into the peers table: %+v", rows)
	}
	// Found at the rendezvous, the same greeting is answered.
	a.know(hb.ID())
	s, _ = hb.NewStream(context.Background(), ha.ID(), protoOffer)
	_ = json.NewEncoder(s).Encode(Offer{Name: "friend"})
	_ = s.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := json.NewDecoder(s).Decode(&theirs); err != nil {
		t.Fatalf("a known hub got no offer back: %v", err)
	}
	rows, _ := a.Store.Peers(context.Background())
	if len(rows) != 1 || rows[0].Name != "friend" || rows[0].Approved {
		t.Fatalf("expected one unapproved row named friend, got %+v", rows)
	}
}

// ask sends one job header (and body after a yes) from b to a and
// returns the reply and whatever followed.
func ask(t *testing.T, hb host.Host, to peer.ID, hdr jobHeader, body string) (jobReply, string) {
	t.Helper()
	s, err := hb.NewStream(context.Background(), to, protoJob)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_ = json.NewEncoder(s).Encode(hdr)
	_ = s.SetReadDeadline(time.Now().Add(5 * time.Second))
	var reply jobReply
	br, err := header(s, &reply)
	if err != nil {
		t.Fatalf("no reply: %v", err)
	}
	if !reply.OK {
		return reply, ""
	}
	_, _ = s.Write([]byte(body))
	_ = s.CloseWrite()
	rest, _ := io.ReadAll(br)
	return reply, string(rest)
}

func TestJobRefusalsAndTheBodyAfterYes(t *testing.T) {
	ctx := context.Background()
	a, ha := node(t)
	_, hb := node(t)
	connect(t, hb, ha)
	hdr := jobHeader{Model: "m", Path: "/api/chat", Size: 4}

	// A stranger's job stream is reset before a byte of it is read.
	s, _ := hb.NewStream(ctx, ha.ID(), protoJob)
	_ = json.NewEncoder(s).Encode(hdr)
	_ = s.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := json.NewDecoder(s).Decode(new(jobReply)); err == nil {
		t.Fatal("a stranger got a job reply")
	}
	s.Close()

	a.know(hb.ID())
	_ = a.Store.SetSetting(ctx, "space.serve", "1")
	if r, _ := ask(t, hb, ha.ID(), hdr, "body"); r.OK || r.Reason != "not approved" {
		t.Fatalf("unapproved peer: %+v", r)
	}
	_ = a.Store.SeenPeer(ctx, hb.ID().String(), "b", true, 2, 20)
	if r, _ := ask(t, hb, ha.ID(), jobHeader{Model: "m", Path: "/api/pull", Size: 4}, "body"); r.OK || !strings.HasPrefix(r.Reason, "not a prompt") {
		t.Fatalf("pull was not refused: %+v", r)
	}
	if r, _ := ask(t, hb, ha.ID(), jobHeader{Model: "m", Path: "/api/chat", Size: bodyMax + 1}, ""); r.OK || r.Reason != "request too large" {
		t.Fatalf("oversized body was not refused: %+v", r)
	}
	// Nothing in the pool holds the model: refused after the header, and
	// the body was never asked for.
	if r, _ := ask(t, hb, ha.ID(), hdr, "body"); r.OK || !strings.HasPrefix(r.Reason, "nothing in the pool") {
		t.Fatalf("empty pool: %+v", r)
	}
	// With the model held: yes, then the body, then the router's answer.
	a.models = func(context.Context) []string { return []string{"m"} }
	r, out := ask(t, hb, ha.ID(), hdr, "body")
	if !r.OK || out != "served /api/chat body" {
		t.Fatalf("happy path: %+v %q", r, out)
	}
}

func TestSendWritesTheBodyAfterTheYes(t *testing.T) {
	ctx := context.Background()
	a, ha := node(t)
	b, hb := node(t)
	connect(t, ha, hb)
	a.know(hb.ID())
	b.know(ha.ID())
	_ = b.Store.SetSetting(ctx, "space.serve", "1")
	_ = b.Store.SeenPeer(ctx, ha.ID().String(), "a", true, 2, 20)
	b.models = func(context.Context) []string { return []string{"m"} }
	rc, err := a.send(ctx, hb.ID(), "b", "m", "/api/generate", []byte(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(rc)
	rc.Close()
	if string(out) != `served /api/generate {"model":"m"}` {
		t.Fatalf("got %q", out)
	}
}

func TestHubWideHourlyAllowance(t *testing.T) {
	ctx := context.Background()
	a, ha := node(t)
	_, hb := node(t)
	connect(t, hb, ha)
	a.know(hb.ID())
	_ = a.Store.SetSetting(ctx, "space.serve", "1")
	_ = a.Store.SetSetting(ctx, "space.per_hour", "1")
	_ = a.Store.SeenPeer(ctx, hb.ID().String(), "b", true, 2, 20)
	// One accepted job from anyone this hour, and the hub is full.
	_, _ = a.Store.RecordPeerJob(ctx, "someone-else", "served", "m", true, "")
	r, _ := ask(t, hb, ha.ID(), jobHeader{Model: "m", Path: "/api/chat", Size: 1}, "x")
	if r.OK || r.Reason != "over the hub's hourly allowance" {
		t.Fatalf("hub-wide allowance not applied: %+v", r)
	}
}

func TestPlaceSendsOnlyToApprovedPeers(t *testing.T) {
	ctx := context.Background()
	a, ha := node(t)
	b, hb := node(t)
	connect(t, ha, hb)
	a.know(hb.ID())
	b.know(ha.ID())
	a.learn(ctx, hb.ID(), Offer{Name: "b", Models: []string{"m"}, Free: true})
	sent := func() int {
		var n int
		_ = a.Store.DB.QueryRow(`SELECT COUNT(*) FROM peer_jobs WHERE direction = 'sent'`).Scan(&n)
		return n
	}
	if _, _, err := a.Place(ctx, "m", "/api/chat", []byte("{}")); err != pool.ErrNoPeer || sent() != 0 {
		t.Fatalf("an unapproved peer was tried: %v, %d sent", err, sent())
	}
	if err := a.Store.SetPeer(ctx, hb.ID().String(), true, 1, 20); err != nil {
		t.Fatal(err)
	}
	// Approved: it is tried, and b (not serving) refuses, which is a
	// sent row with a reason.
	_, _, _ = a.Place(ctx, "m", "/api/chat", []byte("{}"))
	if sent() != 1 {
		t.Fatalf("approved peer was not tried: %d sent", sent())
	}
}

func TestNetConfig(t *testing.T) {
	c := netConfig{Private: true, Bootstrap: []string{"/ip4/1.2.3.4/tcp/4001/p2p/12D3KooWEZXjE41uU4EL2gpkAQeDXYok6wghN7wwNVPF5bwkaNfS", "junk"}}
	if got := c.bootstrapPeers(); len(got) != 1 {
		t.Fatalf("expected the one valid multiaddr, got %d", len(got))
	}
	if len((netConfig{}).bootstrapPeers()) == 0 {
		t.Fatal("public mode has no bootstrap peers")
	}
	if !c.equal(c) || c.equal(netConfig{Private: true}) {
		t.Fatal("equal is wrong")
	}
	if _, _, err := limits(); err != nil {
		t.Fatal(err)
	}
}
