package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adefesioso/homedash/hub/internal/store"
)

// A fake Gitea: the issue list and the create call, nothing else.
func TestProposals(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	type issue struct {
		Title   string `json:"title"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
	}
	var filed []issue
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token tok" || r.URL.Path != "/api/v1/repos/adefesioso/homedash/issues" {
			http.Error(w, "no", http.StatusNotFound)
			return
		}
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(filed)
			return
		}
		var in issue
		_ = json.NewDecoder(r.Body).Decode(&in)
		in.HTMLURL = "https://git.example/adefesioso/homedash/issues/1"
		filed = append(filed, in)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(in)
	}))
	defer srv.Close()
	p := &Proposals{Store: st, Notify: func(string, string, string) {}, Version: "test"}

	if _, err := p.File(ctx, "t", "b"); err == nil || !strings.Contains(err.Error(), "off") {
		t.Fatalf("no token: %v", err)
	}
	_ = st.SetSetting(ctx, "proposals.token", "tok")
	_ = st.SetSetting(ctx, "proposals.repo", srv.URL+"/adefesioso/homedash")
	_ = st.SetSetting(ctx, "proposals.daily", "2")
	if _, err := st.AddHost(ctx, &store.Host{Name: "remote-big", Addr: "192.168.50.10", Port: 22, User: "homedash"}); err != nil {
		t.Fatal(err)
	}

	r, err := p.File(ctx, "Jobs on remote-big need a GPU hint", "remote-big at 192.168.50.10 took two rounds.")
	if err != nil || r.Duplicate || r.URL == "" {
		t.Fatalf("file: %+v %v", r, err)
	}
	got := filed[0]
	if strings.Contains(got.Title+got.Body, "remote-big") || strings.Contains(got.Body, "192.168.50.10") {
		t.Errorf("not redacted: %+v", got)
	}
	if !strings.HasPrefix(got.Title, proposalPrefix) || !strings.Contains(got.Body, "<host> at <address>") {
		t.Errorf("unexpected issue: %+v", got)
	}

	r, err = p.File(ctx, "Jobs on remote-big need a GPU hint", "again")
	if err != nil || !r.Duplicate || len(filed) != 1 {
		t.Fatalf("duplicate: %+v %v (%d filed)", r, err, len(filed))
	}
	if _, err := p.File(ctx, "second", "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.File(ctx, "third", "b"); err == nil || !strings.Contains(err.Error(), "cap") {
		t.Fatalf("cap: %v", err)
	}
}
