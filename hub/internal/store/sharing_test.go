package store

import (
	"context"
	"testing"
)

// The services, fronts, byte-count and workspace tables: the SQL round
// trips, and the shapes the tab and the peers read.
func TestServicesFrontsWorkspaces(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hostID, err := st.AddHost(ctx, &Host{Name: "nas", Addr: "10.0.0.2", Port: 22, User: "homedash", HostKey: "ssh-ed25519 AAAA nas"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.AddHost(ctx, &Host{Name: "pi", Addr: "10.0.0.3", Port: 22, User: "homedash", HostKey: "ssh-ed25519 AAAA pi"})
	if err != nil {
		t.Fatal(err)
	}

	if err := st.PublishService(ctx, "wiki", hostID, 8080, []string{"peerA", "peerB", "peerA"}); err != nil {
		t.Fatal(err)
	}
	svc, err := st.Service(ctx, "wiki")
	if err != nil || svc.Host != "nas" || svc.Port != 8080 || len(svc.Peers) != 2 {
		t.Fatalf("service: %+v %v", svc, err)
	}
	if err := st.PublishService(ctx, "wiki", hostID, 8081, nil); err != nil {
		t.Fatal(err)
	}
	if svc, _ = st.Service(ctx, "wiki"); svc.Port != 8081 || len(svc.Peers) != 0 {
		t.Fatalf("republish did not replace: %+v", svc)
	}
	if err := st.UnpublishService(ctx, "wiki"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Service(ctx, "wiki"); err == nil {
		t.Fatal("unpublished service still there")
	}

	if err := st.SeenPeer(ctx, "peerA", "a", false, 1, 20); err != nil {
		t.Fatal(err)
	}
	if err := st.SeenFront(ctx, "peerA", "photos"); err != nil {
		t.Fatal(err)
	}
	if err := st.SeenFront(ctx, "peerA", "photos"); err != nil {
		t.Fatal(err)
	}
	fs, _ := st.Fronts(ctx, "peerA")
	if len(fs) != 1 || fs[0].Approved {
		t.Fatalf("fronts: %+v", fs)
	}
	if err := st.SetFront(ctx, "peerA", "photos", true, "photos.example.org"); err != nil {
		t.Fatal(err)
	}
	if f, err := st.FrontByHostname(ctx, "photos.example.org"); err != nil || f.PeerID != "peerA" {
		t.Fatalf("by hostname: %+v %v", f, err)
	}
	if err := st.AddPeerBytes(ctx, "peerA", "photos", "fronted", 100); err != nil {
		t.Fatal(err)
	}
	if err := st.AddPeerBytes(ctx, "peerA", "photos", "fronted", 50); err != nil {
		t.Fatal(err)
	}
	if err := st.AddPeerBytes(ctx, "peerA", "wiki", "origin", 7); err != nil {
		t.Fatal(err)
	}
	c, err := st.PeerCounts(ctx, "peerA")
	if err != nil || c.Fronted != [3]int64{150, 150, 150} || c.Origin != [3]int64{7, 7, 7} {
		t.Fatalf("counts: %+v %v", c, err)
	}
	if err := st.ForgetPeer(ctx, "peerA"); err != nil {
		t.Fatal(err)
	}
	if fs, _ = st.Fronts(ctx, "peerA"); len(fs) != 0 {
		t.Fatal("forget kept the fronts")
	}

	cid, err := st.CreateCluster(ctx, "pool", hostID, "/srv/pool", []Member{{HostID: hostID, Path: "/data"}, {HostID: other, Path: "/mnt/disk"}})
	if err != nil {
		t.Fatal(err)
	}
	wid, err := st.CreateWorkspace(ctx, "build", cid, []int64{hostID, other})
	if err != nil {
		t.Fatal(err)
	}
	w, err := st.Workspace(ctx, wid)
	if err != nil || w.Path != "/srv/pool/build" || w.Cluster != "pool" || len(w.Members) != 2 {
		t.Fatalf("workspace: %+v %v", w, err)
	}
	if err := st.RemoveWorkspaceMember(ctx, wid, other); err != nil {
		t.Fatal(err)
	}
	if w, _ = st.Workspace(ctx, wid); len(w.Members) != 1 {
		t.Fatalf("member not removed: %+v", w.Members)
	}
	if err := st.DeleteCluster(ctx, cid); err != nil {
		t.Fatal(err)
	}
	if ws, _ := st.Workspaces(ctx, 0); len(ws) != 0 {
		t.Fatal("workspace outlived its cluster")
	}
}
