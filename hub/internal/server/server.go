// Package server is the hub's HTTP surface: the API under /api and the
// embedded panel everywhere else. Every front door — panel, window and
// CLI — comes through here.
package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/adefesioso/homedash/hub/internal/agent"
	"github.com/adefesioso/homedash/hub/internal/apps"
	"github.com/adefesioso/homedash/hub/internal/auth"
	"github.com/adefesioso/homedash/hub/internal/backup"
	"github.com/adefesioso/homedash/hub/internal/fleet"
	"github.com/adefesioso/homedash/hub/internal/identity"
	"github.com/adefesioso/homedash/hub/internal/mcp"
	"github.com/adefesioso/homedash/hub/internal/peers"
	"github.com/adefesioso/homedash/hub/internal/pool"
	"github.com/adefesioso/homedash/hub/internal/storage"
	"github.com/adefesioso/homedash/hub/internal/store"
	"github.com/adefesioso/homedash/hub/internal/tasks"
)

// Server carries what the handlers need.
type Server struct {
	Store   *store.Store
	Key     *identity.Key
	Agent   *agent.Agent
	Fleet   *fleet.Fleet
	Jobs    *agent.Jobs
	Pool    *pool.Pool
	Tasks   *tasks.Scheduler
	Apps    *apps.Apps
	Storage *storage.Storage
	Backup  *backup.Backup
	Auth    *auth.Auth
	Peers   *peers.Peers
	// Gateway resolves a cluster to its gateway host; nil until clusters exist.
	Gateway apps.Gateway
	// Notify records an event and sends it on.
	Notify fleet.Notify
	// Restart ends the process so systemd starts it again; the entry
	// point sets it. A staged restore is applied on that start.
	Restart func(reason string)
	Version string
	Started time.Time
	UI      fs.FS
	log     *slog.Logger
}

// New wires the routes.
func New(st *store.Store, key *identity.Key, ag *agent.Agent, fl *fleet.Fleet, jobs *agent.Jobs, pl *pool.Pool, ts *tasks.Scheduler, ap *apps.Apps, sto *storage.Storage, bk *backup.Backup, au *auth.Auth, pe *peers.Peers, notify fleet.Notify, ui fs.FS, version string, log *slog.Logger) *Server {
	s := &Server{Store: st, Key: key, Agent: ag, Fleet: fl, Jobs: jobs, Pool: pl, Tasks: ts, Apps: ap, Storage: sto, Gateway: sto.Gateway, Backup: bk, Auth: au, Peers: pe, Notify: notify, Version: version, Started: time.Now(), UI: ui, log: log}
	return s
}

// Handler is the panel and API listener.
func (s *Server) Handler() http.Handler {
	st, ui, version := s.Store, s.UI, s.Version
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/auth/state", s.authState)
	mux.HandleFunc("POST /api/auth/register/begin", s.registerBegin)
	mux.HandleFunc("POST /api/auth/register/finish", s.registerFinish)
	mux.HandleFunc("POST /api/auth/login/begin", s.loginBegin)
	mux.HandleFunc("POST /api/auth/login/finish", s.loginFinish)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("POST /api/auth/cli/grant", s.cliGrant)
	mux.HandleFunc("POST /api/auth/cli/redeem", s.cliRedeem)
	mux.HandleFunc("GET /api/users", s.listUsers)
	mux.HandleFunc("PUT /api/users/{name}/role", s.setRole)
	mux.HandleFunc("DELETE /api/users/{name}", s.deleteUser)
	mux.HandleFunc("GET /api/invites", s.listInvites)
	mux.HandleFunc("POST /api/invites", s.newInvite)
	mux.HandleFunc("DELETE /api/invites/{code}", s.deleteInvite)
	mux.HandleFunc("GET /api/tokens", s.listTokens)
	mux.HandleFunc("POST /api/tokens", s.newToken)
	mux.HandleFunc("DELETE /api/tokens/{name}", s.deleteToken)
	mux.HandleFunc("GET /api/hosts", s.listHosts)
	mux.HandleFunc("POST /api/hosts/enroll", s.newEnrollment)
	mux.HandleFunc("POST /api/hosts/mobile/enroll", s.newMobileEnrollment)
	mux.HandleFunc("GET /api/hosts/{host}", s.getHost)
	mux.HandleFunc("DELETE /api/hosts/{host}", s.removeHost)
	mux.HandleFunc("GET /api/hosts/{host}/metrics", s.hostMetrics)
	mux.HandleFunc("GET /api/hosts/{host}/usage", s.hostUsage)
	mux.HandleFunc("GET /api/usage", s.usageTotals)
	mux.HandleFunc("POST /api/hosts/{host}/lock", s.setLock)
	mux.HandleFunc("PUT /api/hosts/{host}/address", s.setAddress)
	mux.HandleFunc("POST /api/hosts/{host}/run", s.runCommand)
	mux.HandleFunc("PUT /api/hosts/{host}/file", s.writeFile)
	mux.HandleFunc("POST /api/hosts/{host}/credentials", s.updateCredentials)
	mux.HandleFunc("POST /api/hosts/{host}/credentials/revoke", s.revokeCredentials)
	mux.HandleFunc("POST /api/hosts/{host}/reprovision", s.reprovision)
	mux.HandleFunc("PUT /api/hosts/{host}/rebuild-script", s.putRebuildScript)
	mux.HandleFunc("PUT /api/hosts/{host}/agent", s.setAgentModel)
	mux.HandleFunc("GET /api/devices", s.listDevices)
	mux.HandleFunc("POST /api/devices/scan", s.scanNow)
	mux.HandleFunc("PUT /api/devices/{id}", s.nameDevice)
	mux.HandleFunc("DELETE /api/devices/{id}", s.forgetDevice)
	mux.HandleFunc("GET /api/gate", s.gateReasons)
	mux.HandleFunc("GET /api/hosts/{host}/fit", s.hostFit)
	mux.HandleFunc("GET /api/secrets", s.listSecrets)
	mux.HandleFunc("PUT /api/secrets/{name}", s.putSecret)
	mux.HandleFunc("DELETE /api/secrets/{name}", s.deleteSecret)
	mux.HandleFunc("POST /api/hosts/{host}/ollama", s.setOllama)
	mux.HandleFunc("POST /api/hosts/{host}/pool", s.setPoolEnabled)
	mux.HandleFunc("GET /api/models", s.modelGrid)
	mux.HandleFunc("POST /api/models/pull", s.pullModel)
	mux.HandleFunc("POST /api/models/delete", s.deleteModel)
	// The router: the hub is an Ollama endpoint. These are Ollama's own
	// paths, so existing clients point at the hub unchanged.
	router := s.Pool.Serve()
	for _, p := range []string{"/api/tags", "/api/chat", "/api/generate", "/api/embed", "/api/embeddings", "/api/show", "/api/version", "/v1/"} {
		mux.Handle(p, router)
	}
	mux.HandleFunc("GET /api/apps", s.listApps)
	mux.HandleFunc("GET /api/apps/catalog", s.catalog)
	mux.HandleFunc("POST /api/apps/catalog", s.addCatalog)
	mux.HandleFunc("DELETE /api/apps/catalog/{name}", s.deleteCatalog)
	mux.HandleFunc("POST /api/apps/placement", s.placement)
	mux.HandleFunc("POST /api/hosts/{host}/apps", s.deployApp)
	mux.HandleFunc("POST /api/hosts/{host}/apps/{name}", s.appAction)
	mux.HandleFunc("GET /api/hosts/{host}/apps/{name}/file", s.appFile)
	mux.HandleFunc("GET /api/hosts/{host}/apps/{name}/logs", s.appLogs)
	mux.HandleFunc("GET /api/clusters", s.listClusters)
	mux.HandleFunc("POST /api/clusters", s.createCluster)
	mux.HandleFunc("DELETE /api/clusters/{id}", s.deleteCluster)
	mux.HandleFunc("POST /api/clusters/{id}/members", s.addMember)
	mux.HandleFunc("DELETE /api/clusters/{id}/members/{member}", s.removeMember)
	mux.HandleFunc("GET /api/workspaces", s.listWorkspaces)
	mux.HandleFunc("POST /api/workspaces", s.createWorkspace)
	mux.HandleFunc("DELETE /api/workspaces/{id}", s.deleteWorkspace)
	mux.HandleFunc("POST /api/workspaces/{id}/members", s.addWorkspaceMember)
	mux.HandleFunc("DELETE /api/workspaces/{id}/members/{host}", s.removeWorkspaceMember)
	mux.HandleFunc("GET /api/backup", s.backupStatus)
	mux.HandleFunc("POST /api/backup/export", s.backupExport)
	mux.HandleFunc("POST /api/backup/restore", s.backupRestore)
	mux.HandleFunc("GET /api/peers", s.peersTab)
	mux.HandleFunc("PUT /api/peers/{id}", s.setPeer)
	mux.HandleFunc("DELETE /api/peers/{id}", s.forgetPeer)
	mux.HandleFunc("PUT /api/peers/{id}/fronts/{service}", s.setFront)
	mux.HandleFunc("POST /api/services", s.publishService)
	mux.HandleFunc("DELETE /api/services/{name}", s.unpublishService)
	mux.HandleFunc("GET /api/tasks", s.listTasks)
	mux.HandleFunc("POST /api/tasks", s.saveTask)
	mux.HandleFunc("PUT /api/tasks/{id}", s.saveTask)
	mux.HandleFunc("DELETE /api/tasks/{id}", s.deleteTask)
	mux.HandleFunc("POST /api/tasks/{id}/run", s.runTask)
	mux.HandleFunc("GET /api/tasks/{id}/runs", s.taskRuns)
	mux.HandleFunc("GET /api/jobs", s.listJobs)
	mux.HandleFunc("POST /api/jobs", s.startJob)
	mux.HandleFunc("DELETE /api/jobs", s.clearJobs)
	mux.HandleFunc("GET /api/jobs/{id}", s.getJob)
	mux.HandleFunc("GET /api/jobs/{id}/events", s.jobEvents)
	mux.HandleFunc("POST /api/jobs/{id}/rollback", s.rollbackJob)
	mux.HandleFunc("POST /api/jobs/{id}/correct", s.correctJob)
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/hub", s.hub)
	mux.HandleFunc("GET /api/hub/health", s.hubHealth)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("PUT /api/settings", s.putSettings)
	mux.HandleFunc("GET /api/agents", s.agents)
	mux.HandleFunc("POST /api/agents/update", s.updateAgent)
	mux.HandleFunc("GET /api/agents/models", s.agentModels)
	mux.HandleFunc("POST /api/agents/models/refresh", s.refreshAgentModels)
	// The Agents tab's sessions (the windows table): close ends the omp
	// process and keeps the row as history; DELETE removes the row.
	mux.HandleFunc("GET /api/agents/sessions", s.listSessions)
	mux.HandleFunc("POST /api/agents/sessions", s.openSession)
	mux.HandleFunc("DELETE /api/agents/sessions", s.clearSessions)
	mux.HandleFunc("POST /api/agents/sessions/{id}/close", s.closeSession)
	mux.HandleFunc("DELETE /api/agents/sessions/{id}", s.deleteSession)
	mux.HandleFunc("GET /api/agents/sessions/{id}/pty", s.sessionPTY)
	mux.HandleFunc("GET /api/agents/sessions/{id}/history", s.sessionHistory)
	// The omp sessions' tools; a workstation uses the CLI on the API above.
	mux.Handle("/api/mcp", mcp.Handler(st, s.Fleet, s.Jobs, s.Apps, s.Storage, version))
	// Every route above is more specific than this and wins regardless of
	// registration order; anything left under /api/ is a path nothing
	// handles and must say so in plain text, never the panel's index.html
	// (C-4, X-2) — api.js would otherwise try to parse the SPA shell as
	// JSON and report a nonsense error.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no such API path", http.StatusNotFound)
	})
	// A peer's service fronted as a link lives at /~peer/service/, under
	// this panel's sign-in; everything else is the panel.
	panel := spa(ui)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/~") {
			s.linkFront(w, r)
			return
		}
		panel.ServeHTTP(w, r)
	})
	return s.guard(mux)
}

// health is what the launcher asks before opening a window.
func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"ok": true, "version": s.Version})
}

// hub reports the facts about the hub itself: its public key, its uptime,
// where its state lives. Never the private key.
// hub is the hub about itself. name is the machine's hostname: the
// panel shows it so two hubs are never mistaken for one another.
func (s *Server) hub(w http.ResponseWriter, _ *http.Request) {
	name, _ := os.Hostname()
	writeJSON(w, map[string]any{
		"name":      name,
		"version":   s.Version,
		"started":   s.Started.UTC().Format(time.RFC3339),
		"publicKey": strings.TrimSpace(s.Key.AuthorizedKey),
		"statePath": s.Store.Path,
	})
}

// events is the audit log, newest first: 200 rows unless ?limit says
// otherwise (at most 500), and ?before=<id> pages to what came earlier.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	var before int64
	if v := r.URL.Query().Get("before"); v != "" {
		before, _ = strconv.ParseInt(v, 10, 64)
	}
	evs, err := s.Store.Events(ctx, before, limit)
	if err != nil {
		s.log.Error("list events", "err", err)
		http.Error(w, "events unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, evs)
}

// settingKeys is the whole of what the panel may read or write here; a
// key outside it is refused rather than stored.
var settingKeys = []string{"agent.default_model", "agent.remote_model", "agent.rounds", "agent.rules", "jobs.retention", "jobs.timeout", "jobs.sudo", "jobs.memory_max", "jobs.cpu_quota", "notify.target", "notify.disk_percent", "network.scan_minutes", "network.forget_days", "hub.lan_addr", "router.queue", "router.queue_wait", "space.name", "space.enabled", "space.serve", "space.unknown", "space.default_concurrent", "space.default_per_hour", "space.ceiling", "space.per_hour", "space.connections", "space.peer_connections", "space.peer_kbps", "space.hub_name", "space.network", "space.bootstrap", "space.psk", "space.reachable", "space.port", "auth.rpid", "auth.origins"}

// boolSettingKeys are the checkboxes in the Space section of Settings;
// whatever a caller sends ("1", "on", "true" — settingBool in
// internal/peers reads all three) is canonicalised on write to "true"
// or "", so the stored vocabulary is one word (C-11).
var boolSettingKeys = []string{"space.enabled", "space.serve", "space.reachable"}

func settingTruthy(v string) bool {
	return v == "1" || v == "true" || v == "on"
}

// agentModelRE is the one shared shape for a model setting: blank
// (reset to the fleet default, or to the remote's own default) or
// provider/model — homedash/<model> for the house's own pool matches
// the same pattern (C-1, H-8). The part after the first slash is the
// provider's own model id and may contain more slashes itself (an
// OpenRouter model is provider/vendor/model, e.g.
// openrouter/openai/gpt-4o-mini) — only the leading provider segment is
// constrained.
var agentModelRE = regexp.MustCompile(`^[a-z0-9._-]+/\S+$`)

// validAgentModel checks the one rule PUT /api/settings and PUT
// /api/hosts/{id}/agent both enforce.
func validAgentModel(v string) bool {
	return v == "" || (len(v) <= 128 && agentModelRE.MatchString(v))
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	out := map[string]string{}
	for _, k := range settingKeys {
		v, err := s.Store.Setting(r.Context(), k)
		if err != nil {
			http.Error(w, "settings unavailable", http.StatusInternalServerError)
			return
		}
		out[k] = v
	}
	writeJSON(w, out)
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	for k, v := range in {
		if !slices.Contains(settingKeys, k) {
			http.Error(w, "unknown setting "+k, http.StatusBadRequest)
			return
		}
		v = strings.TrimSpace(v)
		if (k == "agent.default_model" || k == "agent.remote_model") && !validAgentModel(v) {
			http.Error(w, k+": blank, or provider/model such as anthropic/claude-sonnet-5 or homedash/qwen2.5:3b", http.StatusBadRequest)
			return
		}
		if slices.Contains(boolSettingKeys, k) {
			if settingTruthy(v) {
				v = "true"
			} else {
				v = ""
			}
		}
		if err := s.Store.SetSetting(r.Context(), k, v); err != nil {
			http.Error(w, "settings unavailable", http.StatusInternalServerError)
			return
		}
	}
	// Rules just changed: rewrite AGENTS.md now, so the hub agent's next
	// window sees it without waiting for a restart.
	if _, ok := in["agent.rules"]; ok {
		if err := s.Agent.WriteContext(r.Context()); err != nil {
			http.Error(w, "settings unavailable", http.StatusInternalServerError)
			return
		}
	}
	s.getSettings(w, r)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// spa serves the built panel; any path it doesn't know is the app shell,
// so the panel's own routing owns the URL.
func spa(ui fs.FS) http.Handler {
	files := http.FS(ui)
	static := http.FileServer(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		// Files in the binary carry no modtime, so the browser cannot
		// revalidate them; say instead what is safe to keep. Vite hashes
		// everything under assets/, so those never change under a name,
		// and index.html is what names them.
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		if f, err := ui.Open(p); err == nil {
			f.Close()
			static.ServeHTTP(w, r)
			return
		}
		r.URL.Path = "/"
		static.ServeHTTP(w, r)
	})
}
