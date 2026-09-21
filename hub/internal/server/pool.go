package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/adefesioso/homedash/hub/internal/pool"
)

func (s *Server) setOllama(w http.ResponseWriter, r *http.Request) {
	h := s.host(w, r)
	if h == nil {
		return
	}
	var in struct {
		Installed bool `json:"installed"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var err error
	if in.Installed {
		err = s.Pool.Install(r.Context(), h)
	} else {
		err = s.Pool.Remove(r.Context(), h)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setPoolEnabled(w http.ResponseWriter, r *http.Request) {
	h := s.host(w, r)
	if h == nil {
		return
	}
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	v := "1"
	if in.Enabled {
		v = ""
	}
	if err := s.Store.SetSetting(r.Context(), "pool.disabled."+strconv.FormatInt(h.ID, 10), v); err != nil {
		http.Error(w, "settings unavailable", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) modelGrid(w http.ResponseWriter, r *http.Request) {
	g, err := s.Pool.Grid(r.Context())
	if err != nil {
		http.Error(w, "hosts unavailable", http.StatusInternalServerError)
		return
	}
	// A connected peer's own offer joins the grid as a read-only column:
	// what it holds, not what this hub owns. No hostId, no free-space
	// figure, no pool switch — those belong only to the peer's own panel.
	if s.Peers != nil {
		rows, err := s.Peers.Rows(r.Context())
		if err != nil {
			s.log.Error("peer rows for models tab", "err", err)
		}
		for _, row := range rows {
			if row.Offer == nil || len(row.Offer.Models) == 0 {
				continue
			}
			models := make([]pool.Model, 0, len(row.Offer.Models))
			for _, name := range row.Offer.Models {
				models = append(models, pool.Model{Name: name})
			}
			host := row.Name
			if host == "" {
				host = row.ID
			}
			g = append(g, pool.Machine{Host: host, Online: row.Connected, Busy: !row.Offer.Free, Models: models, Peer: true})
		}
	}
	writeJSON(w, g)
}

type modelReq struct {
	Host  string `json:"host"`
	Model string `json:"model"`
}

func (s *Server) pullModel(w http.ResponseWriter, r *http.Request) {
	var in modelReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil || in.Model == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	h, err := s.Store.Host(r.Context(), in.Host)
	if err != nil {
		http.Error(w, "no such host", http.StatusNotFound)
		return
	}
	if err := s.Pool.Pull(r.Context(), h, in.Model, w); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
	}
}

func (s *Server) deleteModel(w http.ResponseWriter, r *http.Request) {
	var in modelReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil || in.Model == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	h, err := s.Store.Host(r.Context(), in.Host)
	if err != nil {
		http.Error(w, "no such host", http.StatusNotFound)
		return
	}
	if err := s.Pool.Delete(r.Context(), h, in.Model); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
