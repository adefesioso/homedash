package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/adefesioso/homedash/hub/internal/fleet"
	"github.com/adefesioso/homedash/hub/internal/store"
)

// VaultAddr is where the credential vault listens. Loopback on the hub;
// a remote reaches the same address through the reverse forward on its
// job's SSH connection.
const VaultAddr = "127.0.0.1:8765"

// configDir is PI_CONFIG_DIR: the name of omp's root under HOME, which for
// the hub is the state dir.
const configDir = "omp"

// Agent owns everything omp on the hub.
type Agent struct {
	Store *store.Store
	// StateDir is the hub's state directory and omp's HOME.
	StateDir string
	// MCPURL is the hub's own MCP server, the only tool source a window has.
	MCPURL string
	// RouterAddr is the hub's listener, where the pool answers.
	RouterAddr string
	// Token is the API token the hub's own windows present, like any
	// other client would.
	Token string
	// Notify records and sends a transition; a window a restart ended
	// goes through it too.
	Notify fleet.Notify
	log    *slog.Logger

	mu           sync.Mutex
	installErr   string
	ready        bool
	vaultRunning bool
	ompVersion   string
	windows      map[int64]*Window
}

// New prepares the agent; Start does the work.
func New(st *store.Store, stateDir, mcpURL, routerAddr, token string, notify fleet.Notify, log *slog.Logger) *Agent {
	return &Agent{Store: st, StateDir: stateDir, MCPURL: mcpURL, RouterAddr: routerAddr, Token: token, Notify: notify, log: log, windows: map[int64]*Window{}}
}

func (a *Agent) root() string       { return filepath.Join(a.StateDir, configDir) }
func (a *Agent) bin() string        { return binPath(a.root()) }
func (a *Agent) windowsDir() string { return filepath.Join(a.root(), "windows") }

// env is what every omp process on the hub runs with: HOME is the state
// dir, everything omp writes lands under omp/, and credentials resolve
// through the vault rather than a local store.
func (a *Agent) env() []string {
	return []string{
		"HOME=" + a.StateDir,
		"PI_CONFIG_DIR=" + configDir,
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"LANG=C.UTF-8",
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"OMP_AUTH_BROKER_URL=http://" + VaultAddr,
	}
}

// Start lays out omp/, fetches the pinned binary if it isn't there, and
// keeps the vault up until ctx ends. It returns once the layout exists;
// the fetch and the vault run in the background so the hub serves the
// panel while omp is still arriving.
func (a *Agent) Start(ctx context.Context) error {
	for _, d := range []string{filepath.Join(a.root(), "agent"), a.windowsDir()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	if err := a.writeMCPConfig(); err != nil {
		return err
	}
	if err := a.WriteContext(ctx); err != nil {
		return err
	}
	// The hub's pool as a provider for the hub's own windows: the router
	// is on the same listener as the panel.
	models := "# Written by HomeDash. The hub's pool as a provider: homedash/<model>.\nproviders:\n  homedash:\n    baseUrl: http://" + a.RouterAddr + "/v1\n    api: openai-completions\n    auth: none\n    headers:\n      Authorization: Bearer " + a.Token + "\n    discovery:\n      type: ollama\n      timeoutMs: 5000\n"
	if err := os.WriteFile(filepath.Join(a.root(), "agent", "models.yml"), []byte(models), 0o600); err != nil {
		return err
	}
	// No process survived a restart: every window still marked live is
	// history, and each one gets an event, so a session's loss is
	// something you learn rather than something you notice by its absence.
	ended, err := a.Store.EndAllWindows(ctx)
	if err != nil {
		return err
	}
	for _, w := range ended {
		a.Notify("session.ended", w.Name, "session "+w.Name+" ended: hub restarted")
	}
	go a.run(ctx)
	return nil
}

// writeMCPConfig points the hub's omp at the hub's MCP server and nothing
// else. Rewritten on every start because the listen address may change.
func (a *Agent) writeMCPConfig() error {
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"homedash": map[string]any{"type": "http", "url": a.MCPURL, "headers": map[string]string{"Authorization": "Bearer " + a.Token}},
		},
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(filepath.Join(a.root(), "agent", "mcp.json"), append(b, '\n'), 0o600)
}

func (a *Agent) run(ctx context.Context) {
	for ctx.Err() == nil {
		if installed(ctx, a.root()) {
			break
		}
		err := install(ctx, a.root())
		a.mu.Lock()
		if err != nil {
			a.installErr = err.Error()
		}
		a.mu.Unlock()
		if err == nil {
			break
		}
		a.log.Warn("omp not installed yet, retrying", "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
	}
	if ctx.Err() != nil {
		return
	}
	v := version(ctx, a.root())
	a.mu.Lock()
	a.ready, a.installErr, a.ompVersion = true, "", v
	a.mu.Unlock()
	a.log.Info("omp ready", "version", v, "bin", a.bin())
	a.runVault(ctx)
}

// runVault keeps `omp auth-broker serve` up as a child of the hub. It is
// the only holder of refresh tokens and the only refresher; the hub never
// reads its database.
func (a *Agent) runVault(ctx context.Context) {
	for ctx.Err() == nil {
		cmd := exec.CommandContext(ctx, a.bin(), "auth-broker", "serve", "--bind", VaultAddr)
		cmd.Env = a.env()
		cmd.Dir = a.root()
		cmd.Stdout, cmd.Stderr = nil, os.Stderr
		err := cmd.Start()
		if err == nil {
			a.setVault(true)
			err = cmd.Wait()
		}
		a.setVault(false)
		if ctx.Err() != nil {
			return
		}
		a.log.Warn("vault exited, restarting", "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (a *Agent) setVault(up bool) {
	a.mu.Lock()
	a.vaultRunning = up
	a.mu.Unlock()
}

// VaultToken reads the vault's bearer token, which `auth-broker serve`
// ensures on start. Delivered to a remote over SSH at enrollment.
func (a *Agent) VaultToken() (string, error) {
	b, err := os.ReadFile(filepath.Join(a.root(), "auth-broker.token"))
	if err != nil {
		return "", fmt.Errorf("the vault has not started yet: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

// Status is what the panel shows about the hub's own agent.
type Status struct {
	OmpVersion   string `json:"ompVersion"`
	Ready        bool   `json:"ready"`
	InstallError string `json:"installError,omitempty"`
	VaultRunning bool   `json:"vaultRunning"`
	LiveWindows  int    `json:"liveWindows"`
}

// Status reports the agent's state.
func (a *Agent) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return Status{
		OmpVersion:   a.ompVersion,
		Ready:        a.ready,
		InstallError: a.installErr,
		VaultRunning: a.vaultRunning,
		LiveWindows:  len(a.windows),
	}
}

// Reinstall is the hub side of the "Update oh-my-pi" button under
// Settings > Agents: if omp is already there, its own updater checks
// GitHub's latest release and replaces it in place; otherwise this is
// the bootstrap fetch, for when a corrupted binary or a stuck retry
// needs more than waiting on the minute backoff in run.
func (a *Agent) Reinstall(ctx context.Context) error {
	var err error
	if installed(ctx, a.root()) {
		err = selfUpdate(ctx, a.root())
	} else {
		err = install(ctx, a.root())
	}
	a.mu.Lock()
	if err != nil {
		a.installErr = err.Error()
	} else {
		a.ready, a.installErr, a.ompVersion = true, "", version(ctx, a.root())
	}
	a.mu.Unlock()
	return err
}

// Open names a new window, records it and starts its omp process in a
// PTY. The name is drawn here (names.go), never given: unique among the
// live windows, not against the history a hub restart may have piled up.
func (a *Agent) Open(ctx context.Context) (*store.Window, error) {
	a.mu.Lock()
	ready := a.ready
	a.mu.Unlock()
	if !ready {
		return nil, fmt.Errorf("omp is not installed on the hub yet")
	}
	live, err := a.liveWindows(ctx)
	if err != nil {
		return nil, err
	}
	taken := map[string]bool{}
	for _, w := range live {
		taken[w.Name] = true
	}
	name := drawName()
	for taken[name] {
		name = drawName()
	}
	id, err := a.Store.CreateWindow(ctx, name)
	if err != nil {
		return nil, err
	}
	model, err := a.Store.Setting(ctx, "agent.default_model")
	if err != nil {
		return nil, err
	}
	w, err := a.startWindow(id, model)
	if err != nil {
		_ = a.Store.EndWindow(ctx, id, nil)
		return nil, err
	}
	a.mu.Lock()
	a.windows[id] = w
	a.mu.Unlock()
	return a.Store.Window(ctx, id)
}

// liveWindows is every window not yet ended: on a running hub, the same
// set as a.windows, read from the store so it carries names.
func (a *Agent) liveWindows(ctx context.Context) ([]store.Window, error) {
	ws, err := a.Store.Windows(ctx)
	if err != nil {
		return nil, err
	}
	live := ws[:0]
	for _, w := range ws {
		if w.Ended == "" {
			live = append(live, w)
		}
	}
	return live, nil
}

// Live returns the running window with this id, or nil if it is history.
func (a *Agent) Live(id int64) *Window {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.windows[id]
}

// Close ends a session's process. The row stays as history.
func (a *Agent) Close(id int64) {
	if w := a.Live(id); w != nil {
		w.kill()
	}
}

// ClearHistory removes every ended session from the list.
func (a *Agent) ClearHistory(ctx context.Context) (int64, error) {
	return a.Store.ClearWindows(ctx)
}

// Delete removes a session from the list, ending its process first if it
// is still live. The jobs it started keep their record.
func (a *Agent) Delete(ctx context.Context, id int64) error {
	a.Close(id)
	return a.Store.DeleteWindow(ctx, id)
}

// Stop ends every live window. The vault stops with the context Start was
// given; windows are killed here so none outlives the hub process.
func (a *Agent) Stop() {
	a.mu.Lock()
	ws := make([]*Window, 0, len(a.windows))
	for _, w := range a.windows {
		ws = append(ws, w)
	}
	a.mu.Unlock()
	for _, w := range ws {
		w.kill()
	}
}

// ended is called by a window when its process is gone, with the last of
// its output: the replay buffer becomes the row's scrollback, what the
// History toggle on a finished session shows.
func (a *Agent) ended(id int64, scrollback []byte) {
	a.mu.Lock()
	delete(a.windows, id)
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Store.EndWindow(ctx, id, scrollback); err != nil {
		a.log.Error("end window", "id", id, "err", err)
	}
}
