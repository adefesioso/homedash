package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/adefesioso/homedash/hub/internal/apps"
)

func (s *Server) listApps(w http.ResponseWriter, r *http.Request) {
	stacks, errs, err := s.Apps.List(r.Context())
	if err != nil {
		http.Error(w, "hosts unavailable", http.StatusInternalServerError)
		return
	}
	if stacks == nil {
		stacks = []apps.Stack{}
	}
	writeJSON(w, map[string]any{"stacks": stacks, "errors": errs})
}

func (s *Server) catalog(w http.ResponseWriter, r *http.Request) {
	c, err := s.Apps.Catalog(r.Context())
	if err != nil {
		http.Error(w, "catalog unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, c)
}

func (s *Server) addCatalog(w http.ResponseWriter, r *http.Request) {
	var e apps.Entry
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&e); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.Apps.AddCatalogEntry(r.Context(), e); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteCatalog(w http.ResponseWriter, r *http.Request) {
	if err := s.Apps.DeleteCatalogEntry(r.Context(), r.PathValue("name")); err != nil {
		if errors.Is(err, apps.ErrCatalogNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, "catalog unavailable", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) placement(w http.ResponseWriter, r *http.Request) {
	var needs apps.Needs
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&needs); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	vs, err := s.Apps.Place(r.Context(), needs, s.Gateway)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, vs)
}

func (s *Server) deployApp(w http.ResponseWriter, r *http.Request) {
	h := s.host(w, r)
	if h == nil {
		return
	}
	var in struct {
		Name, Compose, Env string
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// false: this is the panel/CLI door, never an agent's (A-1's compose
	// gate only refuses the MCP door, mcp/apps.go's deploy_stack).
	out, warning, err := s.Apps.Deploy(r.Context(), h, in.Name, in.Compose, in.Env, false)
	if err != nil {
		if isOffline(err) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error()+"\n"+out, http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{"output": out, "warning": warning})
}

func (s *Server) appAction(w http.ResponseWriter, r *http.Request) {
	h := s.host(w, r)
	if h == nil {
		return
	}
	var in struct {
		Action  string `json:"action"`
		Volumes bool   `json:"volumes"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	out, err := s.Apps.Action(r.Context(), h, r.PathValue("name"), in.Action, in.Volumes)
	if err != nil {
		if isOffline(err) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error()+"\n"+out, http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{"output": out})
}

// isOffline reports whether err is apps.Offline: the host's own record
// says it isn't online, so no dial was even attempted and the message
// is that clean sentence rather than a raw connect timeout (A-4).
func isOffline(err error) bool {
	var off *apps.Offline
	return errors.As(err, &off)
}

func (s *Server) appFile(w http.ResponseWriter, r *http.Request) {
	h := s.host(w, r)
	if h == nil {
		return
	}
	compose, env, err := s.Apps.File(r.Context(), h, r.PathValue("name"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{"compose": compose, "env": env})
}

func (s *Server) appLogs(w http.ResponseWriter, r *http.Request) {
	h := s.host(w, r)
	if h == nil {
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	out, err := s.Apps.Logs(r.Context(), h, r.PathValue("name"), n)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(out))
}
