package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"
)

// agents is the state of the hub's own omp: installed, vault up, sessions.
func (s *Server) agents(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.Agent.Status())
}

// agentModels is what the vault can run, grouped by provider: the
// Settings pickers are built from it.
func (s *Server) agentModels(w http.ResponseWriter, r *http.Request) {
	list, err := s.Agent.Models(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, list)
}

// refreshAgentModels is the "Refresh model cache" button under Settings
// > Agents: forces omp to rediscover every provider right away instead
// of waiting out modelsTTL, and returns the fresh listing so the panel
// can update its pickers without a second round trip.
func (s *Server) refreshAgentModels(w http.ResponseWriter, r *http.Request) {
	list, err := s.Agent.RefreshModels(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, list)
}

// updateAgent is the hub side of "Update oh-my-pi" under Settings >
// Agents: forces a fresh fetch and check of the pinned omp binary right
// away. The panel walks every online host's own /update-omp alongside
// this call to bring the whole network to the same pinned version.
func (s *Server) updateAgent(w http.ResponseWriter, r *http.Request) {
	go func() {
		if err := s.Agent.Reinstall(context.Background()); err != nil {
			s.Notify("agent.update_failed", "hub", "hub: omp update failed: "+err.Error())
		}
	}()
	w.WriteHeader(http.StatusNoContent)
}

// listSessions is every session, live or finished, most recently active
// first. Live is whether this hub process still has its omp running.
func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	ws, err := s.Agent.Store.Windows(r.Context())
	if err != nil {
		s.log.Error("list sessions", "err", err)
		http.Error(w, "sessions unavailable", http.StatusInternalServerError)
		return
	}
	type row struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		Created      string `json:"created"`
		LastActivity string `json:"lastActivity"`
		Ended        string `json:"ended,omitempty"`
		Live         bool   `json:"live"`
	}
	out := make([]row, 0, len(ws))
	for _, x := range ws {
		out = append(out, row{x.ID, x.Name, x.Created, x.LastActivity, x.Ended, s.Agent.Live(x.ID) != nil})
	}
	writeJSON(w, out)
}

// openSession opens a window; the hub names it (agent/names.go), so
// the request carries nothing.
func (s *Server) openSession(w http.ResponseWriter, r *http.Request) {
	win, err := s.Agent.Open(r.Context())
	if err != nil {
		s.log.Error("open session", "err", err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, win)
}

// sessionHistory is what a finished session last showed: the replay
// buffer its window kept when the process ended, raw terminal bytes for
// the panel's read-only xterm. 404 for a session that does not exist;
// 409 while it is still live (attach to that instead); an empty body for
// one a hub restart ended, which kept nothing.
func (s *Server) sessionHistory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if s.Agent.Live(id) != nil {
		http.Error(w, "session is still running", http.StatusConflict)
		return
	}
	b, err := s.Agent.Store.WindowScrollback(r.Context(), id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "no such session", http.StatusNotFound)
		return
	case err != nil:
		s.log.Error("session history", "id", id, "err", err)
		http.Error(w, "history unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}

// closeSession ends a live session's omp; the row stays in the list.
func (s *Server) closeSession(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	s.Agent.Close(id)
	w.WriteHeader(http.StatusNoContent)
}

// clearSessions is Clear history: every ended session goes, live ones
// stay. Replies with how many went.
func (s *Server) clearSessions(w http.ResponseWriter, r *http.Request) {
	n, err := s.Agent.ClearHistory(r.Context())
	if err != nil {
		s.log.Error("clear sessions", "err", err)
		http.Error(w, "could not clear the history", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]int64{"cleared": n})
}

// deleteSession removes a session from the list, live or not. Its jobs
// stay on the Jobs tab.
func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	switch err := s.Agent.Delete(r.Context(), id); {
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "no such session", http.StatusNotFound)
	case err != nil:
		s.log.Error("delete session", "id", id, "err", err)
		http.Error(w, "could not delete the session", http.StatusInternalServerError)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// sessionPTY attaches a browser terminal to a live session. Binary frames
// are terminal bytes in both directions; a text frame from the browser is
// a JSON control message, currently only {"resize":{"cols":n,"rows":n}}.
// The PTY outlives this socket: closing it detaches, never kills.
func (s *Server) sessionPTY(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	win := s.Agent.Live(id)
	if win == nil {
		http.Error(w, "session is not running", http.StatusGone)
		return
	}
	// Same-origin only: the default origin check refuses any other page.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	replay, out := win.Attach()
	defer win.Detach(out)
	if len(replay) > 0 {
		if err := conn.Write(ctx, websocket.MessageBinary, replay); err != nil {
			return
		}
	}

	go func() {
		defer cancel()
		for chunk := range out {
			wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Write(wctx, websocket.MessageBinary, chunk)
			wcancel()
			if err != nil {
				return
			}
		}
		// The session ended: say so and let the reader loop finish.
		_ = conn.Close(websocket.StatusNormalClosure, "session ended")
	}()

	// A viewer gets every session as a reader: bytes flow out, none in —
	// keystrokes and resizes both, so a viewer can watch but never resize
	// an admin's pty out from under them (X-8).
	readOnly := false
	if u := s.currentUser(r); u != nil && u.Role != "admin" {
		readOnly = true
	}
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		switch typ {
		case websocket.MessageBinary:
			if readOnly {
				continue
			}
			if err := win.Write(data); err != nil {
				return
			}
		case websocket.MessageText:
			if readOnly {
				continue
			}
			var msg struct {
				Resize *struct{ Cols, Rows uint16 } `json:"resize"`
			}
			if json.Unmarshal(data, &msg) == nil && msg.Resize != nil {
				_ = win.Resize(msg.Resize.Cols, msg.Resize.Rows)
			}
		}
	}
}
