package server

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

func (s *Server) peersTab(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Peers.Rows(r.Context())
	if err != nil {
		http.Error(w, "peers unavailable", http.StatusInternalServerError)
		return
	}
	services, err := s.Store.Services(r.Context())
	if err != nil {
		http.Error(w, "peers unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"status": s.Peers.GetStatus(r.Context()), "peers": rows, "services": services})
}

var serviceNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
var hostnameRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$`)

// refusedPorts are never published, on any host: 22 is SSH, and 7433 is
// the hub's own port — either would let a service front the hub itself.
var refusedPorts = map[int]bool{22: true, 7433: true}

// publishService names one port on one host this hub owns, offered to
// exactly these peers. A name already published is a 409 unless the
// request carries replace: true.
func (s *Server) publishService(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string   `json:"name"`
		Host    string   `json:"host"`
		Port    int      `json:"port"`
		Peers   []string `json:"peers"`
		Replace bool     `json:"replace"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !serviceNameRe.MatchString(in.Name) {
		http.Error(w, "a service name is lowercase letters, digits, - and _", http.StatusBadRequest)
		return
	}
	if in.Port < 1 || in.Port > 65535 {
		http.Error(w, "a service is one port", http.StatusBadRequest)
		return
	}
	if refusedPorts[in.Port] {
		http.Error(w, "the hub's and SSH's ports are never published", http.StatusBadRequest)
		return
	}
	if !in.Replace {
		if _, err := s.Store.Service(r.Context(), in.Name); err == nil {
			http.Error(w, "a service named "+in.Name+" is already published; replace: true to replace it", http.StatusConflict)
			return
		}
	}
	// The host resolves through the store: a service names only a machine
	// this hub owns, so nothing reached through a peer can be re-exported.
	h, err := s.Store.Host(r.Context(), in.Host)
	if err != nil {
		http.Error(w, "no such host", http.StatusNotFound)
		return
	}
	if err := s.Store.PublishService(r.Context(), in.Name, h.ID, in.Port, in.Peers); err != nil {
		http.Error(w, "peers unavailable", http.StatusInternalServerError)
		return
	}
	s.Notify("service.published", in.Name, "service "+in.Name+" published: "+h.Name+":"+strconv.Itoa(in.Port)+" to "+strconv.Itoa(len(in.Peers))+" peer(s)")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) unpublishService(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.Store.UnpublishService(r.Context(), name); err != nil {
		http.Error(w, "peers unavailable", http.StatusInternalServerError)
		return
	}
	s.Notify("service.unpublished", name, "service "+name+" unpublished")
	w.WriteHeader(http.StatusNoContent)
}

// setFront is this hub's side of a peer's service: approve it, and give
// it a hostname to be an ingress rather than a link.
func (s *Server) setFront(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Approved bool   `json:"approved"`
		Hostname string `json:"hostname"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	in.Hostname = strings.ToLower(strings.TrimSpace(in.Hostname))
	if in.Hostname != "" && !hostnameRe.MatchString(in.Hostname) {
		http.Error(w, "a hostname is a DNS name pointed at this hub", http.StatusBadRequest)
		return
	}
	id, service := r.PathValue("id"), r.PathValue("service")
	if err := s.Store.SetFront(r.Context(), id, service, in.Approved, in.Hostname); err != nil {
		http.Error(w, "peers unavailable", http.StatusInternalServerError)
		return
	}
	if in.Approved {
		how := "as a link"
		if in.Hostname != "" {
			how = "as an ingress at " + in.Hostname
		}
		s.Notify("front.approved", service, "fronting "+service+" from "+id+" "+how)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setPeer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Approved      bool `json:"approved"`
		MaxConcurrent int  `json:"maxConcurrent"`
		PerHour       int  `json:"perHour"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if in.MaxConcurrent < 0 {
		http.Error(w, "max concurrent must not be negative (0 means none)", http.StatusBadRequest)
		return
	}
	if in.PerHour < 0 {
		http.Error(w, "per hour must not be negative (0 means none)", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	if err := s.Store.SetPeer(r.Context(), id, in.Approved, in.MaxConcurrent, in.PerHour); err != nil {
		http.Error(w, "peers unavailable", http.StatusInternalServerError)
		return
	}
	if in.Approved {
		s.Notify("peer.approved", id, "peer "+id+" approved")
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) forgetPeer(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.ForgetPeer(r.Context(), r.PathValue("id")); err != nil {
		http.Error(w, "peers unavailable", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
