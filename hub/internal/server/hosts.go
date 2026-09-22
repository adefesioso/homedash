package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/adefesioso/homedash/hub/internal/fleet"
	"github.com/adefesioso/homedash/hub/internal/gate"
	"github.com/adefesioso/homedash/hub/internal/store"
)

// host resolves {host} from the path through the store, or answers 404.
// This is the structural half of the gate: there is no request that
// means "the hub".
func (s *Server) host(w http.ResponseWriter, r *http.Request) *store.Host {
	h, err := s.Store.Host(r.Context(), r.PathValue("host"))
	if err != nil {
		if errors.Is(err, store.ErrNoHost) {
			http.Error(w, "no such host", http.StatusNotFound)
		} else {
			http.Error(w, "hosts unavailable", http.StatusInternalServerError)
		}
		return nil
	}
	return h
}

func (s *Server) listHosts(w http.ResponseWriter, r *http.Request) {
	hs, err := s.Store.Hosts(r.Context())
	if err != nil {
		http.Error(w, "hosts unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, hs)
}

func (s *Server) getHost(w http.ResponseWriter, r *http.Request) {
	if h := s.host(w, r); h != nil {
		writeJSON(w, h)
	}
}

func (s *Server) removeHost(w http.ResponseWriter, r *http.Request) {
	h := s.host(w, r)
	if h == nil {
		return
	}
	s.Fleet.Drop(h.ID)
	if err := s.Store.RemoveHost(r.Context(), h.ID); err != nil {
		http.Error(w, "hosts unavailable", http.StatusInternalServerError)
		return
	}
	s.Notify("host.removed", h.Name, h.Name+" was removed from the hub; nothing on the machine changed")
	w.WriteHeader(http.StatusNoContent)
}

// newEnrollment mints the code and the one line to paste.
func (s *Server) newEnrollment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string `json:"name"`
		RebuildFrom string `json:"rebuildFrom"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	c, line, err := s.Fleet.NewCode(r.Context(), in.Name, in.RebuildFrom)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"code": c.Code, "line": line, "expires": c.Expires})
}

// newMobileEnrollment mints a pairing code for an Android remote: no
// line to paste, since a phone has no shell to paste it into. The
// companion app posts to pairURL to spend the code.
func (s *Server) newMobileEnrollment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	c, pairURL, err := s.Fleet.NewMobileCode(r.Context(), in.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"code": c.Code, "pairUrl": pairURL, "expires": c.Expires})
}

func (s *Server) hostMetrics(w http.ResponseWriter, r *http.Request) {
	h := s.host(w, r)
	if h == nil {
		return
	}
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 || hours > 24*30 {
		hours = 24 * 7
	}
	ms, err := s.Store.HostMetrics(r.Context(), h.ID, hours)
	if err != nil {
		http.Error(w, "metrics unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, ms)
}

func (s *Server) hostUsage(w http.ResponseWriter, r *http.Request) {
	h := s.host(w, r)
	if h == nil {
		return
	}
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 || hours > 24*30 {
		hours = 24 * 7
	}
	us, err := s.Store.HostUsage(r.Context(), h.ID, hours)
	if err != nil {
		http.Error(w, "usage unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, us)
}

func (s *Server) setLock(w http.ResponseWriter, r *http.Request) {
	h := s.host(w, r)
	if h == nil {
		return
	}
	var in struct {
		Locked bool `json:"locked"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.Fleet.SetLock(r.Context(), h, in.Locked); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if in.Locked {
		s.Notify("host.locked", h.Name, h.Name+" locked to the hub's account")
		_ = s.Store.AppendRebuildScript(r.Context(), h.ID, "# HomeDash: lock direct SSH access (set from the panel)\nprintf 'PasswordAuthentication no\\nKbdInteractiveAuthentication no\\nPermitRootLogin no\\nAllowUsers homedash\\n' > /etc/ssh/sshd_config.d/00-homedash-lock.conf && (systemctl reload ssh || systemctl reload sshd)")
	} else {
		s.Notify("host.unlocked", h.Name, h.Name+" unlocked")
		_ = s.Store.AppendRebuildScript(r.Context(), h.ID, "# HomeDash: unlock direct SSH access (set from the panel)\nrm -f /etc/ssh/sshd_config.d/00-homedash-lock.conf && (systemctl reload ssh || systemctl reload sshd)")
	}
	// The card shows what the machine reports, so re-read it now rather
	// than a minute from now.
	go s.Fleet.Sweep(context.Background(), h)
	w.WriteHeader(http.StatusNoContent)
}

// setAddress holds an interface at a static address, or hands it back
// to DHCP (blank address). It answers only once the machine confirmed
// at the new address — up to a minute and a half — with the host as it
// now is, so the card can re-pin without waiting for a poll.
func (s *Server) setAddress(w http.ResponseWriter, r *http.Request) {
	h := s.host(w, r)
	if h == nil {
		return
	}
	var in fleet.Address
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := in.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.Fleet.SetAddress(r.Context(), h, in); err != nil {
		s.Notify("host.address_refused", h.Name, h.Name+": address change not confirmed: "+err.Error())
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if in.CIDR == "" {
		s.Notify("host.address", h.Name, h.Name+"'s "+in.Interface+" is back on DHCP")
		_ = s.Store.AppendRebuildScript(r.Context(), h.ID, "# HomeDash: "+in.Interface+" back on DHCP (set from the panel; re-set from the card's Address section after a rebuild)")
	} else {
		s.Notify("host.address", h.Name, h.Name+" holds "+in.CIDR+" on "+in.Interface)
		_ = s.Store.AppendRebuildScript(r.Context(), h.ID, "# HomeDash: "+in.Interface+" held at "+in.CIDR+" via "+in.Gateway+" (set from the panel; re-set from the card's Address section after a rebuild)")
	}
	go s.Fleet.Sweep(context.Background(), h)
	writeJSON(w, h)
}

// runCommand is the door for the panel and the tools: one gated command
// on one host.
func (s *Server) runCommand(w http.ResponseWriter, r *http.Request) {
	h := s.host(w, r)
	if h == nil {
		return
	}
	var in struct {
		Command  string `json:"command"`
		Timeout  int    `json:"timeoutSeconds"`
		Forwards bool   `json:"forwards"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil || strings.TrimSpace(in.Command) == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if in.Timeout <= 0 || in.Timeout > 3600 {
		in.Timeout = 120
	}
	res, err := s.Fleet.Run(r.Context(), h, in.Command, time.Duration(in.Timeout)*time.Second, in.Forwards)
	if err != nil {
		if gate.IsRefusal(err) {
			http.Error(w, err.Error(), http.StatusForbidden)
		} else {
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
		return
	}
	writeJSON(w, map[string]any{"exitCode": res.ExitCode, "stdout": string(res.Stdout), "stderr": string(res.Stderr), "ms": res.Duration.Milliseconds()})
}

// writeFile is the CLI's door for a file on a host: the gate refuses SSH
// config paths, as it does for a window's write_file.
func (s *Server) writeFile(w http.ResponseWriter, r *http.Request) {
	h := s.host(w, r)
	if h == nil {
		return
	}
	var in struct {
		Path, Content, Mode string
		Sudo                bool
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&in); err != nil || strings.TrimSpace(in.Path) == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if in.Mode == "" {
		in.Mode = "0644"
	}
	if err := s.Fleet.WriteFile(r.Context(), h, in.Path, []byte(in.Content), in.Mode, in.Sudo); err != nil {
		if gate.IsRefusal(err) {
			http.Error(w, err.Error(), http.StatusForbidden)
		} else {
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) updateCredentials(w http.ResponseWriter, r *http.Request) {
	h := s.host(w, r)
	if h == nil {
		return
	}
	if err := s.Fleet.UpdateCredentials(r.Context(), h); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	go s.Fleet.Sweep(context.Background(), h)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) revokeCredentials(w http.ResponseWriter, r *http.Request) {
	h := s.host(w, r)
	if h == nil {
		return
	}
	if err := s.Fleet.RevokeCredentials(r.Context(), h); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// reprovision runs the enrollment layout again on a host: the accounts,
// the key, the cage, the pinned agent. It takes minutes, so it runs in
// the background and ends as an event.
func (s *Server) reprovision(w http.ResponseWriter, r *http.Request) {
	h := s.host(w, r)
	if h == nil {
		return
	}
	go func() {
		if err := s.Fleet.Reprovision(context.Background(), h); err != nil {
			s.Notify("host.reprovision_failed", h.Name, h.Name+": re-provision failed: "+err.Error())
		}
	}()
	w.WriteHeader(http.StatusNoContent)
}

// updateHostOmp replaces just the omp binary on a host, at the fleet's
// pinned version — unlike reprovision, it leaves accounts, the key and
// the cage alone. It takes a minute or two, so it runs in the background
// and ends as an event.
func (s *Server) updateHostOmp(w http.ResponseWriter, r *http.Request) {
	h := s.host(w, r)
	if h == nil {
		return
	}
	go func() {
		if err := s.Fleet.UpdateOmp(context.Background(), h); err != nil {
			s.Notify("host.omp_update_failed", h.Name, h.Name+": omp update failed: "+err.Error())
		}
	}()
	w.WriteHeader(http.StatusNoContent)
}

// putRebuildScript takes the script two ways: `application/json`
// `{"script": "..."}` (the panel) or the raw body as `text/plain` (or no
// content type at all — the CLI's `rebuild -f`). A JSON body that is a
// bare string used to go straight to the store quoted and escaped; that's
// no longer accepted, so a save from either door lands byte-exact.
func (s *Server) putRebuildScript(w http.ResponseWriter, r *http.Request) {
	h := s.host(w, r)
	if h == nil {
		return
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	script := string(b)
	if ct := r.Header.Get("Content-Type"); strings.HasPrefix(ct, "application/json") {
		var bare string
		if json.Unmarshal(b, &bare) == nil {
			http.Error(w, `send {"script": …}`, http.StatusBadRequest)
			return
		}
		var in struct {
			Script string `json:"script"`
		}
		if err := json.Unmarshal(b, &in); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		script = in.Script
	}
	if err := s.Store.SetRebuildScript(r.Context(), h.ID, script); err != nil {
		http.Error(w, "hosts unavailable", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) gateReasons(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, gate.Reasons())
}

// Enrollment is the second listener: on every interface, answering only
// a live code. Everything else is a 404 with nothing to say.
func (s *Server) Enrollment() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /enroll/{code}", func(w http.ResponseWriter, r *http.Request) {
		script, err := s.Fleet.Script(r.Context(), r.PathValue("code"))
		if err != nil || script == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/x-shellscript")
		_, _ = io.WriteString(w, script)
	})
	mux.HandleFunc("POST /enroll/{code}", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		code := r.PathValue("code")
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ec, err := s.Store.EnrollCode(r.Context(), code); err == nil && ec != nil && ec.Kind == "mobile" {
			h, deviceKey, err := s.Fleet.ReportMobile(r.Context(), code, body)
			if err != nil {
				s.log.Warn("mobile pairing refused", "from", ip, "err", err)
				http.NotFound(w, r)
				return
			}
			writeJSON(w, map[string]any{"id": h.ID, "name": h.Name, "deviceKey": deviceKey})
			return
		}
		if _, err := s.Fleet.Report(r.Context(), code, ip, body); err != nil {
			s.log.Warn("enrollment report refused", "from", ip, "err", err)
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// A phone's periodic status push: authenticated by the device key it
	// was handed at pairing, not a panel session — the app is never
	// signed in. Its own listener, apart from the panel and the API, for
	// the same reason the enrollment door is (docs/pooling/hosts.md).
	mux.HandleFunc("POST /mobile/{host}/status", func(w http.ResponseWriter, r *http.Request) {
		h, err := s.Store.Host(r.Context(), r.PathValue("host"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<16))
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		deviceKey := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if err := s.Fleet.SetMobileStatus(r.Context(), h, deviceKey, body); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", http.NotFound)
	return mux
}

func (s *Server) setAgentModel(w http.ResponseWriter, r *http.Request) {
	h := s.host(w, r)
	if h == nil {
		return
	}
	var in struct{ Model string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	in.Model = strings.TrimSpace(in.Model)
	if !validAgentModel(in.Model) {
		http.Error(w, "model: blank, or provider/model such as anthropic/claude-sonnet-5 or homedash/qwen2.5:3b", http.StatusBadRequest)
		return
	}
	if err := s.Fleet.SetAgentModel(r.Context(), h, in.Model); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
