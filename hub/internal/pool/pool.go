// Package pool is the GPUs: every enrolled remote with Ollama on it,
// behind one endpoint the hub hands out. The router speaks Ollama's own
// shape and streams. It picks a machine that is enabled, reachable, idle
// and has the model — one request per machine at a time — and queues to
// a depth you set when all are busy. Turns after the first in a
// conversation go back where the first one went.
//
// This is also the seam part two plugs into: when the local pool can't
// place a request, Peers gets first refusal before the queue.
package pool

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adefesioso/homedash/hub/internal/fleet"
	"github.com/adefesioso/homedash/hub/internal/store"
)

// OllamaPort is where a remote's Ollama answers the hub.
const OllamaPort = 11434

// Peers is part two's side of the seam: a place to send a request the
// local pool can't take. Nil until a space is joined.
type Peers interface {
	// Place returns a stream for the request if a peer accepted it, with
	// the peer's name. On failure the name is the last peer actually
	// asked and err is its own reason, so the caller can say who refused
	// and why; the name is empty and err is ErrNoPeer when nobody was
	// asked at all — no candidate offered the model free.
	Place(ctx context.Context, model string, path string, body []byte) (io.ReadCloser, string, error)
	// Offered is the model names every currently-connected peer is
	// advertising right now, deduped and sorted — what place() can
	// additionally reach through a peer on top of what this pool holds
	// itself. It is folded into what the router lists at /api/tags and
	// /v1/models, so a picker (the panel's or omp's) can name a model
	// only a peer holds. It is never folded into this hub's own outbound
	// Offer or into handleJob's check of what to accept, both of which
	// use Models below and stay local-only: what a hub advertises is what
	// it owns, and a job arriving from a peer is served on this hub's own
	// machines or refused, never relayed to a third hub.
	Offered(ctx context.Context) []string
}

// ErrNoPeer is Peers declining.
var ErrNoPeer = errors.New("no peer took the job")

// Pool is the router and the model grid.
type Pool struct {
	Store  *store.Store
	Fleet  *fleet.Fleet
	Peers  Peers
	Notify fleet.Notify
	Log    *slog.Logger

	mu     sync.Mutex
	busy   map[int64]bool  // host id → serving a request
	freed  chan struct{}   // closed and replaced on every release: the queue's wake-up
	homes  map[string]home // conversation prefix hash → where it went
	tagsC  map[int64]tagsEntry
	client *http.Client
}

type tagsEntry struct {
	at     time.Time
	models []Model
}

type home struct {
	hostID int64
	peer   string
	at     time.Time
}

// New makes a pool.
func New(st *store.Store, fl *fleet.Fleet, notify fleet.Notify, log *slog.Logger) *Pool {
	return &Pool{Store: st, Fleet: fl, Notify: notify, Log: log, busy: map[int64]bool{}, homes: map[string]home{}, tagsC: map[int64]tagsEntry{}, freed: make(chan struct{}),
		client: &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 4, ResponseHeaderTimeout: 10 * time.Minute}}}
}

func ollamaURL(h *store.Host) string {
	return "http://" + net.JoinHostPort(h.Addr, strconv.Itoa(OllamaPort))
}

// --- install ----------------------------------------------------------

// Install puts Ollama on a host with its own installer and makes it
// listen for the hub. Remove takes all of it away.
func (p *Pool) Install(ctx context.Context, h *store.Host) error {
	script := `set -e
if ! command -v ollama >/dev/null 2>&1; then curl -fsSL https://ollama.com/install.sh | sh >/dev/null; fi
mkdir -p /etc/systemd/system/ollama.service.d
printf '[Service]\nEnvironment=OLLAMA_HOST=0.0.0.0:11434\n' > /etc/systemd/system/ollama.service.d/homedash.conf
systemctl daemon-reload; systemctl enable --now ollama; systemctl restart ollama
for i in $(seq 1 30); do curl -fs http://127.0.0.1:11434/api/version >/dev/null && exit 0; sleep 1; done
echo "ollama did not come up" >&2; exit 1`
	r, err := p.Fleet.Exec.Run(ctx, fleet.Target(h), []string{"sudo", "-n", "sh", "-s"}, strings.NewReader(script))
	if err != nil {
		return err
	}
	if r.ExitCode != 0 {
		return fmt.Errorf("install ollama: %s", lastLine(r.Stderr))
	}
	_ = p.Store.AppendRebuildScript(ctx, h.ID, "# HomeDash: Ollama (installed from the panel)\n"+script)
	p.Notify("ollama.installed", h.Name, "Ollama installed on "+h.Name)
	go p.Fleet.Sweep(context.Background(), h)
	return nil
}

// Remove uninstalls Ollama and its models from a host.
func (p *Pool) Remove(ctx context.Context, h *store.Host) error {
	script := `set -e
systemctl disable --now ollama 2>/dev/null || true
rm -f /etc/systemd/system/ollama.service /etc/systemd/system/ollama.service.d/homedash.conf
rm -rf /usr/local/lib/ollama /usr/share/ollama /etc/systemd/system/ollama.service.d
rm -f /usr/local/bin/ollama
userdel ollama 2>/dev/null || true; groupdel ollama 2>/dev/null || true
systemctl daemon-reload`
	r, err := p.Fleet.Exec.Run(ctx, fleet.Target(h), []string{"sudo", "-n", "sh", "-s"}, strings.NewReader(script))
	if err != nil {
		return err
	}
	if r.ExitCode != 0 {
		return fmt.Errorf("remove ollama: %s", lastLine(r.Stderr))
	}
	_ = p.Store.AppendRebuildScript(ctx, h.ID, "# HomeDash: Ollama removed (from the panel)\n"+script)
	p.Notify("ollama.removed", h.Name, "Ollama removed from "+h.Name)
	go p.Fleet.Sweep(context.Background(), h)
	return nil
}

// --- the grid ---------------------------------------------------------

// Machine is one column of the Models tab. A peer's column (Peer true)
// is read-only: HostID, Enabled and Free carry nothing, because pulling
// to or removing from a machine you don't own isn't a thing the panel
// can offer — only the peer's own panel can.
type Machine struct {
	Host    string  `json:"host"`
	HostID  int64   `json:"hostId"`
	Online  bool    `json:"online"`
	Busy    bool    `json:"busy"`
	Models  []Model `json:"models"`
	Error   string  `json:"error,omitempty"`
	Enabled bool    `json:"enabled"`
	Free    int64   `json:"free"` // bytes free where Ollama keeps models
	Peer    bool    `json:"peer,omitempty"`
}

// Model is one cell.
type Model struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// Grid asks every Ollama machine what it holds, live.
func (p *Pool) Grid(ctx context.Context) ([]Machine, error) {
	hosts, err := p.Store.Hosts(ctx)
	if err != nil {
		return nil, err
	}
	out := []Machine{}
	var wg sync.WaitGroup
	var mu sync.Mutex
	off := p.disabled(ctx)
	for i := range hosts {
		h := &hosts[i]
		var f fleet.Facts
		_ = json.Unmarshal(h.Facts, &f)
		if !f.Ollama.Installed {
			continue
		}
		m := Machine{Host: h.Name, HostID: h.ID, Online: h.Status == "online", Enabled: !off[h.ID], Models: []Model{}}
		for _, mt := range f.Mounts {
			if mt.Path == "/" || mt.Path == "/usr" {
				m.Free = mt.Free
			}
		}
		p.mu.Lock()
		m.Busy = p.busy[h.ID]
		p.mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			ms, err := p.tags(ctx, h)
			if err != nil {
				m.Error = err.Error()
			} else {
				m.Models = ms
			}
			mu.Lock()
			out = append(out, m)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out, nil
}

func (p *Pool) enabled(ctx context.Context, h *store.Host) bool {
	v, _ := p.Store.Setting(ctx, "pool.disabled."+strconv.FormatInt(h.ID, 10))
	return v != "1"
}

// disabled reads every pool switch in one query, for a pass over the
// whole fleet.
func (p *Pool) disabled(ctx context.Context) map[int64]bool {
	out := map[int64]bool{}
	m, _ := p.Store.SettingsWithPrefix(ctx, "pool.disabled.")
	for k, v := range m {
		if id, err := strconv.ParseInt(strings.TrimPrefix(k, "pool.disabled."), 10, 64); err == nil && v == "1" {
			out[id] = true
		}
	}
	return out
}

// tags is what a machine holds, cached for a few seconds so the router
// does not ask on every attempt.
func (p *Pool) tags(ctx context.Context, h *store.Host) ([]Model, error) {
	p.mu.Lock()
	if e, ok := p.tagsC[h.ID]; ok && time.Since(e.at) < 10*time.Second {
		p.mu.Unlock()
		return e.models, nil
	}
	p.mu.Unlock()
	ms, err := p.fetchTags(ctx, h)
	if err == nil {
		p.mu.Lock()
		p.tagsC[h.ID] = tagsEntry{at: time.Now(), models: ms}
		p.mu.Unlock()
	}
	return ms, err
}

func (p *Pool) fetchTags(ctx context.Context, h *store.Host) ([]Model, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ollamaURL(h)+"/api/tags", nil)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Models []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]Model, 0, len(body.Models))
	for _, m := range body.Models {
		out = append(out, Model{Name: m.Name, Size: m.Size})
	}
	return out, nil
}

// Pull streams a model pull on one host straight through to w.
func (p *Pool) Pull(ctx context.Context, h *store.Host, model string, w http.ResponseWriter) error {
	body, _ := json.Marshal(map[string]any{"model": model, "stream": true})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ollamaURL(h)+"/api/pull", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_ = p.Store.AppendRebuildScript(ctx, h.ID, "ollama pull "+shellQuote(model))
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(resp.StatusCode)
	stream(w, resp.Body)
	p.mu.Lock()
	delete(p.tagsC, h.ID)
	p.mu.Unlock()
	return nil
}

// Delete removes a model from one host.
func (p *Pool) Delete(ctx context.Context, h *store.Host, model string) error {
	body, _ := json.Marshal(map[string]any{"model": model})
	req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, ollamaURL(h)+"/api/delete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete %s on %s: %s", model, h.Name, strings.TrimSpace(string(b)))
	}
	_ = p.Store.AppendRebuildScript(ctx, h.ID, "ollama rm "+shellQuote(model)+" || true")
	p.mu.Lock()
	delete(p.tagsC, h.ID)
	p.mu.Unlock()
	return nil
}

// --- the router -------------------------------------------------------

// Decision is what every response carries: where it was placed and the
// one reason it went there. A peer that was asked and said no still
// gets named, even when the answer ends up local: Peer and PeerError
// carry its name and its own words, whatever else the decision says.
type Decision struct {
	Host      string `json:"host,omitempty"`
	Peer      string `json:"peer,omitempty"`
	Reason    string `json:"reason"`
	PeerError string `json:"peerError,omitempty"`
}

// JobPaths is the whole of what may cross to a peer: a prompt. It is the
// pool's own set, shared rather than copied — peers.go accepts an
// inbound job by the same rule this router uses to decide whether one
// may leave at all. Ollama's other paths — pull, create, push, copy,
// delete, and the lookups below — never cross.
var JobPaths = map[string]bool{
	"/api/chat": true, "/api/generate": true, "/api/embed": true, "/api/embeddings": true,
	"/v1/chat/completions": true, "/v1/completions": true, "/v1/embeddings": true,
}

// Serve is the router. It takes any Ollama-shaped request under the
// prefix, places it, and streams the answer back with the decision in a
// header.
func (p *Pool) Serve() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<20))
		if err != nil {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		switch path {
		case "/api/tags", "/v1/models":
			p.serveTags(w, r, path)
			return
		case "/api/version":
			writeJSON(w, map[string]string{"version": "homedash"})
			return
		case "/api/show":
			// A model lookup, not a prompt: answered from whatever the
			// local pool already knows, and never asked of a peer.
			p.serveShow(w, r, body)
			return
		}
		model, messages := parseRequest(body)
		if model == "" {
			http.Error(w, "the request names no model", http.StatusBadRequest)
			return
		}
		p.place(w, r, path, model, messages, body)
	})
}

// serveShow answers /api/show from whichever local, enabled machine
// holds the model — Ollama's own reply, proxied through unchanged. A
// model nothing local holds is Ollama's own 404, in its own shape,
// never a wait for a machine to free up and never a peer's business.
func (p *Pool) serveShow(w http.ResponseWriter, r *http.Request, body []byte) {
	ctx := r.Context()
	var in struct {
		Model string `json:"model"`
		Name  string `json:"name"`
	}
	_ = json.Unmarshal(body, &in)
	model := in.Model
	if model == "" {
		model = in.Name
	}
	if model == "" {
		http.Error(w, "the request names no model", http.StatusBadRequest)
		return
	}
	hosts, _ := p.Store.Hosts(ctx)
	off := p.disabled(ctx)
	for i := range hosts {
		h := &hosts[i]
		var f fleet.Facts
		_ = json.Unmarshal(h.Facts, &f)
		if !f.Ollama.Installed || h.Status != "online" || off[h.ID] {
			continue
		}
		ms, err := p.tags(ctx, h)
		if err != nil || !hasModel(ms, model) {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, ollamaURL(h)+"/api/show", bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := p.client.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()
		for k, v := range resp.Header {
			if k == "Content-Type" || k == "Content-Length" {
				w.Header()[k] = v
			}
		}
		w.WriteHeader(resp.StatusCode)
		stream(w, resp.Body)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "model '" + model + "' not found"})
}

// serveTags is the union of every machine's models — what this hub
// advertises to its space — plus, for a picker on this hub (the panel,
// or omp's own discovery), whatever a connected peer is offering right
// now: place() already reaches those through a peer when nothing local
// holds them, so a picker naming one is naming a real choice, not a
// dead end.
func (p *Pool) serveTags(w http.ResponseWriter, r *http.Request, path string) {
	names := p.Models(r.Context())
	if p.Peers != nil {
		names = union(names, p.Peers.Offered(r.Context()))
	}
	if path == "/v1/models" {
		data := []map[string]any{}
		for _, n := range names {
			data = append(data, map[string]any{"id": n, "object": "model", "owned_by": "homedash"})
		}
		writeJSON(w, map[string]any{"object": "list", "data": data})
		return
	}
	models := []map[string]any{}
	for _, n := range names {
		models = append(models, map[string]any{"name": n, "model": n})
	}
	writeJSON(w, map[string]any{"models": models})
}

// Models is the set of models the pool can serve, sorted.
func (p *Pool) Models(ctx context.Context) []string {
	grid, _ := p.Grid(ctx)
	seen := map[string]bool{}
	var out []string
	for _, m := range grid {
		if !m.Online || !m.Enabled {
			continue
		}
		for _, x := range m.Models {
			if !seen[x.Name] {
				seen[x.Name] = true
				out = append(out, x.Name)
			}
		}
	}
	sortStrings(out)
	return out
}

// place decides and proxies. The decision order is the README's: the
// conversation's home if it still says yes, then a free local machine
// with the model, then a peer, then the queue.
func (p *Pool) place(w http.ResponseWriter, r *http.Request, path, model string, messages []json.RawMessage, body []byte) {
	ctx := r.Context()
	key, prefixKeys := conversationKeys(messages)
	// A job that arrived from a peer is served here or refused, never
	// passed on: the inbound path has no route to the peer fallback. Nor
	// does anything that isn't a prompt — jobPaths is the whole of what a
	// peer may be asked, and place only ever sees a path in that set or
	// one of the local-only lookups serveShow/serveTags answer first.
	peers := p.Peers
	if r.Header.Get("X-HomeDash-Local-Only") != "" || !JobPaths[path] {
		peers = nil
	}
	deadline := time.Now().Add(time.Duration(p.setting(ctx, "router.queue_wait", 120)) * time.Second)
	queued := false
	defer func() {
		if queued {
			p.dequeue()
		}
	}()
	for attempt := 0; ; attempt++ {
		// Taken before looking, so a release between the look and the
		// wait below is not missed.
		p.mu.Lock()
		freed := p.freed
		p.mu.Unlock()
		// 1. The home, if it is still there and still says yes.
		if hm, ok := p.home(prefixKeys); ok {
			if hm.peer != "" && peers != nil {
				if ok, _ := p.tryPeer(w, r, path, model, body, key, "this was the conversation's home from turn one"); ok {
					return
				}
				// The home peer said no or its stream broke; place this
				// turn as if it were the first, same as when a local home
				// disappears below.
			} else if hm.hostID != 0 {
				if h := p.acquire(ctx, hm.hostID); h != nil {
					p.proxy(w, r, path, model, h, body, Decision{Host: h.Name, Reason: "this was the conversation's home from turn one"}, key)
					return
				}
			}
		}
		// 2. A free local machine with the model.
		h, reason := p.pick(ctx, model)
		if h != nil {
			p.proxy(w, r, path, model, h, body, Decision{Host: h.Name, Reason: reason}, key)
			return
		}
		// 3. A peer gets first refusal before the queue — and a hub whose
		// own machines cannot serve the model at all goes straight to its
		// space: an empty grid and a space name is a gateway.
		why := "the local pool was busy"
		if reason != "busy" {
			why = reason
		}
		var peerErr error
		if peers != nil {
			var ok bool
			if ok, peerErr = p.tryPeer(w, r, path, model, body, key, why); ok {
				return
			}
		}
		if reason != "busy" {
			dec := Decision{Reason: reason}
			msg := "no machine in the pool holds " + model + " (" + reason + ")"
			if pe, ok := peerErr.(*peerError); ok {
				dec.Peer, dec.PeerError = pe.peer, pe.err.Error()
				msg = pe.peer + " refused: " + pe.err.Error() + "; " + msg
			}
			dj, _ := json.Marshal(dec)
			w.Header().Set("X-HomeDash-Decision", string(dj))
			http.Error(w, msg, http.StatusNotFound)
			return
		}
		// 4. The queue, to the depth set, for as long as set.
		if !queued {
			if !p.enqueue() {
				http.Error(w, "the pool is busy and the queue is full", http.StatusServiceUnavailable)
				return
			}
			queued = true
		}
		if time.Now().After(deadline) {
			http.Error(w, "the pool stayed busy for the whole wait", http.StatusServiceUnavailable)
			return
		}
		// Sleep until a machine is released — or a few seconds, for the
		// ways a machine becomes free that are not a release: a host
		// coming online, a switch flipped, a model pulled.
		select {
		case <-ctx.Done():
			return
		case <-freed:
		case <-time.After(5 * time.Second):
		}
	}
}

var queued int
var queueMu sync.Mutex

func (p *Pool) enqueue() bool {
	queueMu.Lock()
	defer queueMu.Unlock()
	depth := p.setting(context.Background(), "router.queue", 8)
	if queued >= depth {
		return false
	}
	queued++
	return true
}

func (p *Pool) dequeue() {
	queueMu.Lock()
	if queued > 0 {
		queued--
	}
	queueMu.Unlock()
}

// pick finds an enabled, online, idle machine holding the model. The
// reason is "busy" when there was one but it was serving, otherwise why
// nothing fits.
func (p *Pool) pick(ctx context.Context, model string) (*store.Host, string) {
	hosts, err := p.Store.Hosts(ctx)
	if err != nil {
		return nil, "hosts unavailable"
	}
	busyOne := false
	holders := 0
	off := p.disabled(ctx)
	for i := range hosts {
		h := &hosts[i]
		var f fleet.Facts
		_ = json.Unmarshal(h.Facts, &f)
		if !f.Ollama.Installed || h.Status != "online" || off[h.ID] {
			continue
		}
		ms, err := p.tags(ctx, h)
		if err != nil {
			continue
		}
		if !hasModel(ms, model) {
			continue
		}
		holders++
		if got := p.take(h); got != nil {
			reason := "the only machine with " + model + " was free"
			if holders > 1 || len(hosts) > 1 {
				reason = h.Name + " was free and holds " + model
			}
			return got, reason
		}
		busyOne = true
	}
	if busyOne {
		return nil, "busy"
	}
	if holders == 0 {
		return nil, "nothing in the pool holds " + model
	}
	return nil, "busy"
}

// acquire marks a host busy if it is free, checking it is still online
// and switched on. Release with release.
func (p *Pool) acquire(ctx context.Context, hostID int64) *store.Host {
	h, err := p.Store.Host(ctx, strconv.FormatInt(hostID, 10))
	if err != nil || h.Status != "online" || !p.enabled(ctx, h) {
		return nil
	}
	return p.take(h)
}

// take is acquire for a host already read and checked this pass.
func (p *Pool) take(h *store.Host) *store.Host {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.busy[h.ID] {
		return nil
	}
	p.busy[h.ID] = true
	return h
}

// release frees a host and wakes everything queued on the pool.
func (p *Pool) release(hostID int64) {
	p.mu.Lock()
	delete(p.busy, hostID)
	close(p.freed)
	p.freed = make(chan struct{})
	p.mu.Unlock()
}

// proxy streams one request to one machine and remembers the home.
func (p *Pool) proxy(w http.ResponseWriter, r *http.Request, path, model string, h *store.Host, body []byte, d Decision, key string) {
	defer p.release(h.ID)
	req, err := http.NewRequestWithContext(r.Context(), r.Method, ollamaURL(h)+path, bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		http.Error(w, h.Name+": "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if key != "" {
		p.remember(key, home{hostID: h.ID, at: time.Now()})
	}
	dj, _ := json.Marshal(d)
	w.Header().Set("X-HomeDash-Decision", string(dj))
	for k, v := range resp.Header {
		if k == "Content-Type" || k == "Content-Length" {
			w.Header()[k] = v
		}
	}
	w.WriteHeader(resp.StatusCode)
	m := &meter{r: resp.Body}
	stream(w, m)
	if resp.StatusCode < 400 {
		p.record(r, m, h.Name, false, model)
	}
}

// peerError is a peer that was actually asked and said no, or whose
// stream broke before any of the answer arrived — as opposed to nobody
// being asked at all, which carries no peer to name.
type peerError struct {
	peer string
	err  error
}

func (e *peerError) Error() string { return e.peer + ": " + e.err.Error() }

// tryPeer asks Peers to place the job and, on a yes, streams the answer
// back with the decision in a header. ok is true only once the peer's
// own answer has actually started arriving: WriteHeader is held back
// until the first byte (or the peer's own error) is in hand, so a relay
// reset right after the peer's yes is a failure here, never an empty
// 200 the client reads as success. On a refusal or a broken stream, err
// names the peer that was asked and its own words — nil when nobody was
// asked at all, so the caller knows there is nothing to report.
func (p *Pool) tryPeer(w http.ResponseWriter, r *http.Request, path, model string, body []byte, key, reason string) (bool, error) {
	rc, name, err := p.Peers.Place(r.Context(), model, path, body)
	if err != nil {
		if name == "" {
			return false, nil
		}
		return false, &peerError{peer: name, err: err}
	}
	defer rc.Close()
	m := &meter{r: rc}
	buf := make([]byte, 32<<10)
	n, rerr := m.Read(buf)
	if n == 0 && rerr != nil {
		return false, &peerError{peer: name, err: rerr}
	}
	defer p.record(r, m, name, true, model)
	if key != "" {
		p.remember(key, home{peer: name, at: time.Now()})
	}
	dj, _ := json.Marshal(Decision{Peer: name, Reason: reason})
	w.Header().Set("X-HomeDash-Decision", string(dj))
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	if n > 0 {
		if _, werr := w.Write(buf[:n]); werr != nil {
			return true, nil
		}
		if fl != nil {
			fl.Flush()
		}
	}
	if rerr == nil {
		stream(w, m)
	}
	return true, nil
}

// --- one decision per conversation -----------------------------------

// conversationKeys hashes the history. The key for this turn is the hash
// of all its messages; the prefix keys are the hashes after each message
// before the last, which is where an earlier turn's key would be found.
func conversationKeys(messages []json.RawMessage) (string, []string) {
	if len(messages) == 0 {
		return "", nil
	}
	hsh := sha256.New()
	var prefixes []string
	for i, m := range messages {
		hsh.Write(m)
		hsh.Write([]byte{0})
		sum := hex.EncodeToString(hsh.Sum(nil))
		if i < len(messages)-1 {
			prefixes = append(prefixes, sum)
		} else {
			return sum, prefixes
		}
	}
	return "", nil
}

func (p *Pool) home(prefixes []string) (home, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(prefixes) - 1; i >= 0; i-- {
		if hm, ok := p.homes[prefixes[i]]; ok && time.Since(hm.at) < 2*time.Hour {
			return hm, true
		}
	}
	return home{}, false
}

func (p *Pool) remember(key string, hm home) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.homes) > 10000 {
		for k, v := range p.homes {
			if time.Since(v.at) > time.Hour {
				delete(p.homes, k)
			}
		}
	}
	p.homes[key] = hm
}

// --- helpers ----------------------------------------------------------

// parseRequest reads the model and the messages from an Ollama or
// OpenAI-shaped body. A /api/generate prompt is one message.
func parseRequest(body []byte) (string, []json.RawMessage) {
	var in struct {
		Model    string            `json:"model"`
		Messages []json.RawMessage `json:"messages"`
		Prompt   json.RawMessage   `json:"prompt"`
	}
	if json.Unmarshal(body, &in) != nil {
		return "", nil
	}
	if len(in.Messages) == 0 && len(in.Prompt) > 0 {
		in.Messages = []json.RawMessage{in.Prompt}
	}
	return in.Model, in.Messages
}

func hasModel(ms []Model, name string) bool {
	for _, m := range ms {
		if m.Name == name || m.Name == name+":latest" {
			return true
		}
	}
	return false
}

func (p *Pool) setting(ctx context.Context, key string, def int) int {
	v, _ := p.Store.Setting(ctx, key)
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
		return n
	}
	return def
}

// stream copies with a flush per chunk, so a streaming answer streams.
func stream(w http.ResponseWriter, r io.Reader) {
	fl, _ := w.(http.Flusher)
	buf := make([]byte, 32<<10)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func lastLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// union is a ∪ b, deduped and sorted.
func union(a, b []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(a)+len(b))
	for _, xs := range [2][]string{a, b} {
		for _, x := range xs {
			if !seen[x] {
				seen[x] = true
				out = append(out, x)
			}
		}
	}
	sortStrings(out)
	return out
}

func sortStrings(xs []string) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}
