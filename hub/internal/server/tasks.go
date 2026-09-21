package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/adefesioso/homedash/hub/internal/store"
)

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	ts, err := s.Store.Tasks(r.Context())
	if err != nil {
		http.Error(w, "tasks unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, ts)
}

func (s *Server) saveTask(w http.ResponseWriter, r *http.Request) {
	// TimeoutSeconds is decoded separately from store.Task's own field of
	// the same name (shallower wins the JSON tag) so a blank one — the
	// key left out of the body — reads as nil and keeps the default,
	// while an explicit value out of bounds is refused rather than
	// silently clamped.
	var in struct {
		store.Task
		TimeoutSeconds *int `json:"timeoutSeconds"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	t := in.Task
	if id := r.PathValue("id"); id != "" {
		t.ID, _ = strconv.ParseInt(id, 10, 64)
	}
	switch {
	case in.TimeoutSeconds == nil:
		t.TimeoutS = 0 // Validate defaults a non-positive timeout to 600
	case *in.TimeoutSeconds < 1:
		http.Error(w, "a task's timeout is at least 1 second (blank: 600)", http.StatusBadRequest)
		return
	case *in.TimeoutSeconds > 86400:
		http.Error(w, "a task's timeout is at most 86400 seconds (a day)", http.StatusBadRequest)
		return
	default:
		t.TimeoutS = *in.TimeoutSeconds
	}
	saved, err := s.Tasks.Save(r.Context(), &t)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, saved)
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.Tasks.Delete(r.Context(), id); err != nil {
		http.Error(w, "tasks unavailable", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) runTask(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	t, err := s.Store.Task(r.Context(), id)
	if err != nil {
		http.Error(w, "no such task", http.StatusNotFound)
		return
	}
	if !t.Enabled {
		http.Error(w, "task is off", http.StatusConflict)
		return
	}
	go s.Tasks.Fire(context.Background(), id, true)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) taskRuns(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	runs, err := s.Store.TaskRuns(r.Context(), id, 30)
	if err != nil {
		http.Error(w, "tasks unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, runs)
}
