package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/adefesioso/homedash/hub/internal/store"
)

// listDevices is the Network tab: every device with its sightings, and
// what each remote's last scan could see.
func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	ds, err := s.Store.Devices(r.Context())
	if err != nil {
		http.Error(w, "devices unavailable", http.StatusInternalServerError)
		return
	}
	scans, err := s.Store.HostScans(r.Context())
	if err != nil {
		http.Error(w, "devices unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, struct {
		Devices  []store.Device   `json:"devices"`
		Scanners []store.HostScan `json:"scanners"`
	}{ds, scans})
}

func (s *Server) nameDevice(w http.ResponseWriter, r *http.Request) {
	d, err := s.Store.Device(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, "no such device", http.StatusNotFound)
		return
	}
	var in struct{ Name, Kind string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if len(in.Name) > 64 {
		http.Error(w, "a device name is at most 64 characters", http.StatusBadRequest)
		return
	}
	if err := s.Store.NameDevice(r.Context(), d.ID, in.Name, in.Kind); err != nil {
		http.Error(w, "devices unavailable", http.StatusInternalServerError)
		return
	}
	d, _ = s.Store.Device(r.Context(), r.PathValue("id"))
	writeJSON(w, d)
}

func (s *Server) forgetDevice(w http.ResponseWriter, r *http.Request) {
	d, err := s.Store.Device(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, "no such device", http.StatusNotFound)
		return
	}
	if err := s.Store.ForgetDevice(r.Context(), d.ID); err != nil {
		http.Error(w, "devices unavailable", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// scanNow runs one scan of every online host before answering.
func (s *Server) scanNow(w http.ResponseWriter, r *http.Request) {
	s.Fleet.ScanAll(r.Context())
	s.listDevices(w, r)
}
