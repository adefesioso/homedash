package server

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// Secrets are written here and never read back: the panel sees names
// and grants, the tools see names, the job door sees values.
func (s *Server) listSecrets(w http.ResponseWriter, r *http.Request) {
	secs, err := s.Store.Secrets(r.Context())
	if err != nil {
		http.Error(w, "secrets unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, secs)
}

func (s *Server) putSecret(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Value string  `json:"value"`
		Hosts []int64 `json:"hosts"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	if err := s.Store.SetSecret(r.Context(), name, in.Value, in.Hosts); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.Notify("secret.set", name, "secret "+name+" set for "+grantText(in.Hosts))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteSecret(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.Store.DeleteSecret(r.Context(), name); err != nil {
		http.Error(w, "secrets unavailable", http.StatusInternalServerError)
		return
	}
	s.Notify("secret.deleted", name, "secret "+name+" deleted")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) hostFit(w http.ResponseWriter, r *http.Request) {
	h := s.host(w, r)
	if h == nil {
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	out, err := s.Fleet.Fit(r.Context(), h, n)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, out)
}

func grantText(hosts []int64) string {
	if len(hosts) == 0 {
		return "every host"
	}
	return strconv.Itoa(len(hosts)) + " host(s)"
}
