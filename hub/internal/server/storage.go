package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/adefesioso/homedash/hub/internal/store"
)

func (s *Server) listClusters(w http.ResponseWriter, r *http.Request) {
	st, err := s.Storage.Statuses(r.Context())
	if err != nil {
		http.Error(w, "storage unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, st)
}

func (s *Server) createCluster(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string `json:"name"`
		Gateway string `json:"gateway"`
		Path    string `json:"path"`
		Members []struct {
			Host string `json:"host"`
			Path string `json:"path"`
		} `json:"members"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	gw, err := s.Store.Host(r.Context(), in.Gateway)
	if err != nil {
		http.Error(w, "no such gateway host", http.StatusNotFound)
		return
	}
	var members []store.Member
	for _, m := range in.Members {
		h, err := s.Store.Host(r.Context(), m.Host)
		if err != nil {
			http.Error(w, "no such host "+m.Host, http.StatusNotFound)
			return
		}
		members = append(members, store.Member{HostID: h.ID, Host: h.Name, Path: m.Path})
	}
	c, err := s.Storage.Create(r.Context(), in.Name, gw, in.Path, members)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, c)
}

func (s *Server) cluster(w http.ResponseWriter, r *http.Request) *store.Cluster {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	c, err := s.Store.Cluster(r.Context(), id)
	if err != nil {
		http.Error(w, "no such cluster", http.StatusNotFound)
		return nil
	}
	return c
}

func (s *Server) addMember(w http.ResponseWriter, r *http.Request) {
	c := s.cluster(w, r)
	if c == nil {
		return
	}
	var in struct{ Host, Path string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	h, err := s.Store.Host(r.Context(), in.Host)
	if err != nil {
		http.Error(w, "no such host", http.StatusNotFound)
		return
	}
	if err := s.Storage.AddMember(r.Context(), c, h, in.Path); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) {
	c := s.cluster(w, r)
	if c == nil {
		return
	}
	mid, _ := strconv.ParseInt(r.PathValue("member"), 10, 64)
	if err := s.Storage.RemoveMember(r.Context(), c, mid); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteCluster(w http.ResponseWriter, r *http.Request) {
	c := s.cluster(w, r)
	if c == nil {
		return
	}
	if err := s.Storage.Delete(r.Context(), c); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) workspace(w http.ResponseWriter, r *http.Request) *store.Workspace {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	ws, err := s.Store.Workspace(r.Context(), id)
	if err != nil {
		http.Error(w, "no such workspace", http.StatusNotFound)
		return nil
	}
	return ws
}

func (s *Server) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	ws, err := s.Storage.WorkspaceStatuses(r.Context(), 0)
	if err != nil {
		http.Error(w, "storage unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, ws)
}

func (s *Server) createWorkspace(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string   `json:"name"`
		Cluster string   `json:"cluster"`
		Hosts   []string `json:"hosts"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	c, err := s.Store.ClusterByName(r.Context(), in.Cluster)
	if err != nil {
		http.Error(w, "no such cluster", http.StatusNotFound)
		return
	}
	var hosts []*store.Host
	for _, ref := range in.Hosts {
		h, err := s.Store.Host(r.Context(), ref)
		if err != nil {
			http.Error(w, "no such host "+ref, http.StatusNotFound)
			return
		}
		hosts = append(hosts, h)
	}
	ws, err := s.Storage.CreateWorkspace(r.Context(), c, in.Name, hosts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, ws)
}

func (s *Server) deleteWorkspace(w http.ResponseWriter, r *http.Request) {
	ws := s.workspace(w, r)
	if ws == nil {
		return
	}
	if err := s.Storage.DeleteWorkspace(r.Context(), ws); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) addWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	ws := s.workspace(w, r)
	if ws == nil {
		return
	}
	var in struct{ Host string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	h, err := s.Store.Host(r.Context(), in.Host)
	if err != nil {
		http.Error(w, "no such host", http.StatusNotFound)
		return
	}
	if err := s.Storage.AddWorkspaceMember(r.Context(), ws, h); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	ws := s.workspace(w, r)
	if ws == nil {
		return
	}
	h, err := s.Store.Host(r.Context(), r.PathValue("host"))
	if err != nil {
		http.Error(w, "no such host", http.StatusNotFound)
		return
	}
	if err := s.Storage.RemoveWorkspaceMember(r.Context(), ws, h); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
