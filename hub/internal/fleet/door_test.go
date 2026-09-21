package fleet

import (
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/adefesioso/homedash/hub/internal/store"
)

func TestDoorServesRouterAndNothingElse(t *testing.T) {
	f := &Fleet{Log: slog.Default(), Notify: func(string, string, string) {},
		Router: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "router:"+r.URL.Path) })}
	addr, closeDoor, err := f.door(nil, &store.Host{ID: 1, Name: "x"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDoor()
	get := func(p string) (int, string) {
		resp, err := http.Get("http://" + addr + p)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if c, b := get("/api/tags"); c != 200 || b != "router:/api/tags" {
		t.Errorf("/api/tags: %d %q", c, b)
	}
	if c, b := get("/v1/models"); c != 200 || b != "router:/v1/models" {
		t.Errorf("/v1/models: %d %q", c, b)
	}
	if c, _ := get("/api/hosts"); c != 404 {
		t.Errorf("/api/hosts should be 404, got %d", c)
	}
}
