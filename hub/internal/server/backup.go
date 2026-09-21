package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/adefesioso/homedash/hub/internal/backup"
)

func (s *Server) backupStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.Backup.Status(r.Context()))
}

// backupExport streams the state directory as one encrypted file, the
// download Settings offers. The passphrase comes in the body, never a URL.
func (s *Server) backupExport(w http.ResponseWriter, r *http.Request) {
	var in struct{ Passphrase string }
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in)
	if strings.TrimSpace(in.Passphrase) == "" {
		http.Error(w, "an export needs a passphrase", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+backup.FileName()+`"`)
	if err := s.Backup.Export(r.Context(), w, in.Passphrase); err != nil {
		// The body is untouched until the export is built, so a refusal
		// can still be an error reply.
		w.Header().Del("Content-Disposition")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.Notify("backup.export", "backup", "the state directory was exported")
}

// backupRestore takes an export (multipart: file, passphrase), stages it
// beside the live state, then restarts the hub so the next start applies
// it. Open without an account while the hub has none — the rebuild case.
func (s *Server) backupRestore(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "send the export as multipart: file and passphrase", http.StatusBadRequest)
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "send the export as multipart: file and passphrase", http.StatusBadRequest)
		return
	}
	defer f.Close()
	if err := backup.Stage(s.Backup.StateDir, f, r.FormValue("passphrase")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.Notify("backup.restore", "backup", "an export was staged; the hub is restarting to apply it")
	w.WriteHeader(http.StatusAccepted)
	go s.Restart("restore")
}
