package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/adefesioso/homedash/hub/internal/pool"
)

// caller marks a router request on the hub's own listener with who is
// asking, for the served counts: the hub agent's own token is `hub`,
// anyone else signed in is `client:<name>`. A client's own value for
// the header is never kept.
func (s *Server) caller(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		who := ""
		if u := s.currentUser(r); u != nil {
			who = "client:" + strings.TrimPrefix(u.Name, "token:")
			if u.Name == "token:hub-agent" {
				who = "hub"
			}
		}
		r.Header.Set(pool.CallerHeader, who)
		next.ServeHTTP(w, r)
	})
}

// usageHours is a Usage request's window: a week unless it names one
// up to thirty days.
func usageHours(r *http.Request) int {
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 || hours > 24*30 {
		hours = 24 * 7
	}
	return hours
}

// hubUsage is the hub agent's own spend over the window, per model per
// hour, read from its windows' session files.
func (s *Server) hubUsage(w http.ResponseWriter, r *http.Request) {
	us, err := s.Store.HubUsage(r.Context(), usageHours(r))
	if err != nil {
		http.Error(w, "usage unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, us)
}

// servedUsage is what the router's machines and peers answered over the
// window, per server, caller and model per hour.
func (s *Server) servedUsage(w http.ResponseWriter, r *http.Request) {
	vs, err := s.Store.ServedUsage(r.Context(), usageHours(r))
	if err != nil {
		http.Error(w, "usage unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, vs)
}
