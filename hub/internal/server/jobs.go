package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/adefesioso/homedash/hub/internal/agent"
)

// clearJobs is Clear history: every finished job goes with its events and
// its snapshot; running jobs stay. Replies with how many went.
func (s *Server) clearJobs(w http.ResponseWriter, r *http.Request) {
	n, err := s.Jobs.Clear(r.Context())
	if err != nil {
		s.log.Error("clear jobs", "err", err)
		http.Error(w, "could not clear the history", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]int{"cleared": n})
}

// usageTotals is the Usage tab's overview: every host's token totals
// over the window, in one call, so the panel can compare them without
// stitching per-host requests together.
func (s *Server) usageTotals(w http.ResponseWriter, r *http.Request) {
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 || hours > 24*30 {
		hours = 24 * 7
	}
	us, err := s.Store.UsageTotals(r.Context(), hours)
	if err != nil {
		http.Error(w, "usage unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, us)
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	var hostID int64
	if ref := r.URL.Query().Get("host"); ref != "" {
		h, err := s.Store.Host(r.Context(), ref)
		if err != nil {
			http.Error(w, "no such host", http.StatusNotFound)
			return
		}
		hostID = h.ID
	}
	js, err := s.Store.Jobs(r.Context(), hostID, 200)
	if err != nil {
		http.Error(w, "jobs unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, js)
}

func (s *Server) startJob(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Host     string `json:"host"`
		Cwd      string `json:"cwd"`
		Text     string `json:"text"`
		WindowID int64  `json:"windowId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	h, err := s.Store.Host(r.Context(), in.Host)
	if err != nil {
		http.Error(w, "no such host", http.StatusNotFound)
		return
	}
	j, err := s.Jobs.Start(r.Context(), h, in.Cwd, in.Text, in.WindowID)
	if err != nil {
		status := http.StatusBadRequest
		var off *agent.OfflineError
		if errors.As(err, &off) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, j)
}

func (s *Server) jobID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	id, ok := s.jobID(w, r)
	if !ok {
		return
	}
	j, err := s.Store.Job(r.Context(), id)
	if err != nil {
		http.Error(w, "no such job", http.StatusNotFound)
		return
	}
	writeJSON(w, j)
}

func (s *Server) jobEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := s.jobID(w, r)
	if !ok {
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	evs, err := s.Store.JobEvents(r.Context(), id, after, 500)
	if err != nil {
		http.Error(w, "jobs unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, evs)
}

func (s *Server) rollbackJob(w http.ResponseWriter, r *http.Request) {
	id, ok := s.jobID(w, r)
	if !ok {
		return
	}
	j, err := s.Jobs.Rollback(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, j)
}

func (s *Server) correctJob(w http.ResponseWriter, r *http.Request) {
	id, ok := s.jobID(w, r)
	if !ok {
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	j, err := s.Jobs.Correct(r.Context(), id, in.Text)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, j)
}
