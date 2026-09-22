// Package fleet is the hub's side of the machines it owns: enrollment,
// the heartbeat that settles whether each one is really there, the
// numbers it keeps per sweep, the lock, and the one gated door every
// command to a remote goes through.
package fleet

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/adefesioso/homedash/hub/internal/gate"
	"github.com/adefesioso/homedash/hub/internal/remote"
	"github.com/adefesioso/homedash/hub/internal/store"
)

//go:embed facts.sh
var factsScript string

//go:embed enroll.sh
var enrollScript string

//go:embed layout.sh
var layoutScript string

//go:embed omp.sh
var ompScript string

// RouterForwardAddr is where, on a remote's loopback, the hub's router
// answers while a job's connection is up.
const RouterForwardAddr = "127.0.0.1:11435"

// modelsYML makes the hub's router a provider on the remote.
const modelsYML = `# Written by HomeDash. The hub's pool as a provider: homedash/<model>.
providers:
  homedash:
    baseUrl: http://` + RouterForwardAddr + `/v1
    api: openai-completions
    auth: none
    discovery:
      type: ollama
      timeoutMs: 5000
`

var (
	enrollTmpl = template.Must(template.New("enroll").Parse(enrollScript))
	layoutTmpl = template.Must(template.New("layout").Parse(layoutScript))
)

// Notify is how a transition leaves the fleet: it is recorded as an event
// and sent to the notification target, never blocking what reported it.
type Notify func(kind, subject, message string)

// Fleet owns the hosts.
type Fleet struct {
	Store  *store.Store
	Exec   *remote.Executor
	PubKey string // the hub's public key, one authorized_keys line
	// EnrollURL is where a remote fetches its script: the hub's LAN
	// address and the enrollment port.
	EnrollURL func() string
	// OmpVersion and OmpRelease pin the agent a remote installs;
	// LlmfitVersion and LlmfitRelease pin llmfit beside it.
	OmpVersion, OmpRelease       string
	LlmfitVersion, LlmfitRelease string
	// VaultToken reads the vault's bearer token, delivered over SSH.
	VaultToken func() (string, error)
	// VaultAddr is the loopback address the vault answers on, here and —
	// through the reverse forward — on a remote.
	VaultAddr string
	// Router is the pool's handler: Ollama's paths. The job door serves
	// it to a remote as 127.0.0.1:11435. Set once the pool exists.
	Router http.Handler
	Notify Notify
	Log    *slog.Logger

	fullMu sync.Mutex
	full   map[string]bool // "host:mount" → above the threshold last sweep
	held   held            // one kept SSH client per host, for service traffic
}

// Target is the executor's view of a host.
func Target(h *store.Host) remote.Target {
	return remote.Target{Addr: net.JoinHostPort(h.Addr, strconv.Itoa(h.Port)), User: h.User, HostKey: h.HostKey}
}

// Facts is the parsed shape of what facts.sh prints, as far as the hub
// reads it; the panel gets the whole object.
type Facts struct {
	Hostname string  `json:"hostname"`
	Cores    int     `json:"cores"`
	MemTotal int64   `json:"memTotal"`
	MemUsed  int64   `json:"memUsed"`
	Load1    float64 `json:"load1"`
	Mounts   []struct {
		Path   string `json:"path"`
		FS     string `json:"fs"`
		Size   int64  `json:"size"`
		Free   int64  `json:"free"`
		Source string `json:"source"`
	} `json:"mounts"`
	GPU *struct {
		Name string `json:"name"`
		Busy bool   `json:"busy"`
	} `json:"gpu"`
	Docker *string `json:"docker"`
	Ollama struct {
		Installed bool `json:"installed"`
		Running   bool `json:"running"`
	} `json:"ollama"`
	Lock *struct {
		Locked bool `json:"locked"`
	} `json:"lock"`
	Agent *struct {
		Version     string `json:"version"`
		Model       string `json:"model"`
		SnapshotAge *int64 `json:"snapshotAge"`
		Account     bool   `json:"account"`
		Cage        bool   `json:"cage"`
	} `json:"agent"`
}

// SystemDevices is what the host last reported its system and swap sit
// on, walked down to the whole disk (facts' systemDevices): the list the
// gate refuses disk tools on. nil when the host has not reported it —
// facts from before this field, or none yet — and then the gate refuses
// every disk.
func SystemDevices(h *store.Host) []string {
	var raw struct {
		SystemDevices []string `json:"systemDevices"`
	}
	if json.Unmarshal(h.Facts, &raw) != nil || len(raw.SystemDevices) == 0 {
		return nil
	}
	return raw.SystemDevices
}

// --- enrollment -------------------------------------------------------

// NewCode mints an enrollment code and returns the line to paste.
func (f *Fleet) NewCode(ctx context.Context, name string, rebuildFrom string) (*store.EnrollCode, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "", errors.New("a remote needs a name")
	}
	var from int64
	if rebuildFrom != "" {
		h, err := f.Store.Host(ctx, rebuildFrom)
		if err != nil {
			return nil, "", err
		}
		from = h.ID
	}
	code := randomCode()
	c, err := f.Store.NewEnrollCode(ctx, code, name, "compute", from)
	if err != nil {
		return nil, "", err
	}
	return c, fmt.Sprintf("curl -fsSL %s/enroll/%s | sudo sh", f.EnrollURL(), code), nil
}

// layout renders layout.sh: the idempotent body enrollment and
// Reprovision share. The vault token is not in it: that arrives over SSH.
func (f *Fleet) layout(ctx context.Context) (string, error) {
	model := f.RemoteModel(ctx)
	var buf bytes.Buffer
	err := layoutTmpl.Execute(&buf, map[string]string{
		"ModelsYML": strings.TrimSpace(modelsYML), "Hook": gate.Hook,
		"SecretHelper": strings.TrimSpace(secretHelper), "SudoHelper": strings.TrimSpace(sudoHelper),
	})
	// The values the layout reads as shell variables come first, so the
	// same text runs from enrollment (which sets them) and from
	// Reprovision (which sets them here).
	head := "PUBKEY=" + shq(strings.TrimSpace(f.PubKey)) + "\nOMP_VERSION=" + shq(f.OmpVersion) + "\nOMP_RELEASE=" + shq(f.OmpRelease) +
		"\nLLMFIT_VERSION=" + shq(f.LlmfitVersion) + "\nLLMFIT_RELEASE=" + shq(f.LlmfitRelease) + "\nDEFAULT_MODEL=" + shq(model) + "\n"
	return head + buf.String(), err
}

// Script renders the enrollment script for a live code, or "" if the
// code is not live.
func (f *Fleet) Script(ctx context.Context, code string) (string, error) {
	c, err := f.Store.EnrollCode(ctx, code)
	if err != nil || c == nil || c.Kind == "mobile" {
		return "", err
	}
	layout, err := f.layout(ctx)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	err = enrollTmpl.Execute(&buf, map[string]string{
		"HubURL": f.EnrollURL(), "Code": code, "Name": c.Name, "Layout": layout, "Facts": factsScript,
	})
	return buf.String(), err
}

// Reprovision runs the layout on a host already enrolled, as root over
// SSH, then delivers credentials again: the way a remote from before the
// current layout, or behind a pinned version, catches up.
func (f *Fleet) Reprovision(ctx context.Context, h *store.Host) error {
	layout, err := f.layout(ctx)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	c, err := f.Exec.Dial(ctx, Target(h))
	if err != nil {
		return err
	}
	defer c.Close()
	r, err := remote.RunOn(ctx, c, []string{"sudo", "-n", "sh", "-s"}, strings.NewReader("set -eu\n"+layout))
	if err != nil {
		return err
	}
	if r.ExitCode != 0 {
		return fmt.Errorf("layout exited %d: %s", r.ExitCode, tail(r.Stderr))
	}
	skipped, err := f.deliverCredentials(ctx, c, h)
	if err != nil {
		f.Notify("agent.credentials_failed", h.Name, h.Name+": credential pull failed: "+err.Error())
		return err
	}
	msg := h.Name + ": layout applied"
	if skipped {
		msg += "; credential snapshot skipped — too little memory for the agent to run"
	}
	f.Notify("host.reprovisioned", h.Name, msg)
	f.Sweep(ctx, h)
	return nil
}

// UpdateOmp replaces just the omp binary on a host, at the fleet's pinned
// OmpVersion — the minimal move, unlike Reprovision, which also redoes
// accounts, the key and the cage.
func (f *Fleet) UpdateOmp(ctx context.Context, h *store.Host) error {
	script := "set -eu\nOMP_VERSION=" + shq(f.OmpVersion) + "\nOMP_RELEASE=" + shq(f.OmpRelease) + "\n" + ompScript
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	r, err := f.Exec.Run(ctx, Target(h), []string{"sudo", "-n", "sh", "-s"}, strings.NewReader(script))
	if err != nil {
		return err
	}
	if r.ExitCode != 0 {
		return fmt.Errorf("omp update exited %d: %s", r.ExitCode, tail(r.Stderr))
	}
	f.Notify("host.omp_updated", h.Name, h.Name+": omp updated")
	go f.Sweep(context.Background(), h)
	return nil
}

// Report is the remote checking in at the end of its script. The address
// it came from is the address the hub pins; the rest is what it said.
func (f *Fleet) Report(ctx context.Context, code, fromIP string, body []byte) (*store.Host, error) {
	c, err := f.Store.EnrollCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errors.New("code is not live")
	}
	if c.Kind == "mobile" {
		return nil, errors.New("this code is for a mobile remote; use the pairing endpoint")
	}
	var in struct {
		Name    string          `json:"name"`
		Port    int             `json:"port"`
		HostKey string          `json:"hostKey"`
		Facts   json.RawMessage `json:"facts"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, fmt.Errorf("report: %w", err)
	}
	if _, _, _, _, err := ssh.ParseAuthorizedKey([]byte(in.HostKey)); err != nil {
		return nil, fmt.Errorf("report: host key: %w", err)
	}
	if in.Port == 0 {
		in.Port = 22
	}
	if !json.Valid(in.Facts) {
		in.Facts = json.RawMessage("{}")
	}
	ok, err := f.Store.UseEnrollCode(ctx, code)
	if err != nil || !ok {
		return nil, errors.New("code already used")
	}
	h := &store.Host{Name: c.Name, Addr: fromIP, Port: in.Port, User: "homedash", HostKey: strings.TrimSpace(in.HostKey), Facts: in.Facts}
	if c.RebuildFrom != 0 {
		if from, err := f.Store.Host(ctx, strconv.FormatInt(c.RebuildFrom, 10)); err == nil {
			h.RebuildScript = from.RebuildScript
		}
	}
	id, err := f.Store.AddHost(ctx, h)
	if err != nil {
		return nil, err
	}
	h.ID = id
	f.Notify("host.enrolled", h.Name, fmt.Sprintf("%s enrolled from %s", h.Name, fromIP))
	// The rest — the vault token, the rebuild script, the first heartbeat —
	// runs over the connection the hub now can open, and must not hold the
	// remote's curl.
	go f.finish(context.Background(), h, c.RebuildFrom != 0)
	return h, nil
}

// finish is everything enrollment does over SSH once the remote has
// authorized the hub's key: deliver the vault token, pull the first
// credential snapshot, run the rebuild script if there is one, and settle
// the card with a real heartbeat.
func (f *Fleet) finish(ctx context.Context, h *store.Host, rebuild bool) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	c, err := f.Exec.Dial(ctx, Target(h))
	if err != nil {
		f.Log.Warn("enrollment finish: cannot connect", "host", h.Name, "err", err)
		f.Sweep(ctx, h)
		return
	}
	defer c.Close()
	if _, err := f.deliverCredentials(ctx, c, h); err != nil {
		f.Notify("agent.credentials_failed", h.Name, h.Name+": credential pull failed: "+err.Error())
	}
	if rebuild && strings.TrimSpace(h.RebuildScript) != "" {
		r, err := remote.RunOn(ctx, c, []string{"sudo", "-n", "sh", "-s"}, strings.NewReader(h.RebuildScript))
		switch {
		case err != nil:
			f.Notify("host.rebuild_failed", h.Name, h.Name+": rebuild script did not run: "+err.Error())
		case r.ExitCode != 0:
			f.Notify("host.rebuild_failed", h.Name, fmt.Sprintf("%s: rebuild script exited %d: %s", h.Name, r.ExitCode, tail(r.Stderr)))
		default:
			f.Notify("host.rebuilt", h.Name, h.Name+": rebuild script ran")
		}
	}
	f.Sweep(ctx, h)
}

// piClassFloor mirrors agent.memoryFloor: below it the kernel kills omp
// before the first token, so there is nothing for a snapshot to serve
// (H-15).
const piClassFloor = 1400 << 20

// deliverCredentials mints this host's own vault token, writes it under
// the agent account and, with the vault proxy reachable through the
// reverse forward, has omp pull a snapshot. This is also what Update
// credentials on a card does. skipped is true when the snapshot pull was
// left out for a Pi-class box rather than run and found failing.
func (f *Fleet) deliverCredentials(ctx context.Context, c *ssh.Client, h *store.Host) (skipped bool, err error) {
	if _, err := f.VaultToken(); err != nil {
		return false, err
	}
	tok := randomToken()
	if err := f.Store.SetVaultToken(ctx, h.ID, tok); err != nil {
		return false, err
	}
	h.VaultToken = tok
	home := "/home/" + gate.AgentAccount
	if err := remote.WriteFile(ctx, c, home+"/.omp/auth-broker.token", []byte(tok), "0600", true); err != nil {
		return false, err
	}
	// The hub's own pool as a provider: `homedash/<model>` runs on the
	// fleet's GPUs, through the router forward the job's connection carries.
	if err := remote.WriteFile(ctx, c, home+"/.omp/agent/models.yml", []byte(modelsYML), "0600", true); err != nil {
		return false, err
	}
	owner := gate.AgentAccount + ":" + gate.AgentAccount
	if r, err := remote.RunOn(ctx, c, []string{"sudo", "-n", "chown", owner, home + "/.omp/auth-broker.token", home + "/.omp/agent/models.yml"}, nil); err != nil {
		return false, err
	} else if r.ExitCode != 0 {
		return false, fmt.Errorf("chown: %s", tail(r.Stderr))
	}
	var facts Facts
	_ = json.Unmarshal(h.Facts, &facts)
	if facts.MemTotal > 0 && facts.MemTotal < piClassFloor {
		return true, nil
	}
	stop, err := f.ForwardsFor(ctx, c, h, 0)
	if err != nil {
		return false, err
	}
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	// `omp models` is the cheapest command that discovers auth storage,
	// which in broker mode fetches /v1/snapshot and writes the encrypted
	// cache; `auth-broker status` only pings. Its output is not needed.
	r, err := remote.RunOn(ctx, c, []string{"sudo", "-n", "-u", gate.AgentAccount, "sh", "-lc", "cd && rm -f .omp/agent/models.db .omp/agent/models.db-* && OMP_AUTH_BROKER_URL=http://" + f.VaultAddr + " omp models >/dev/null 2>&1; test -f .omp/cache/auth-broker-snapshot.enc"}, nil)
	if err != nil {
		return false, err
	}
	if r.ExitCode != 0 {
		return false, fmt.Errorf("omp did not write a credential snapshot: %s", tail(r.Stderr))
	}
	return false, nil
}

// RevokeCredentials empties the host's vault token: the proxy answers
// nothing for it until Update credentials mints another. The card reads
// "credentials revoked" until then (H-9).
func (f *Fleet) RevokeCredentials(ctx context.Context, h *store.Host) error {
	if err := f.Store.SetVaultToken(ctx, h.ID, ""); err != nil {
		return err
	}
	if err := f.Store.SetCredentialsRevoked(ctx, h.ID, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	f.Notify("agent.credentials_revoked", h.Name, h.Name+": vault access revoked")
	return nil
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Forwards puts the hub's vault and this connection's job door on the
// remote's loopback for as long as c lasts: 127.0.0.1:8765 and
// 127.0.0.1:11435 there resolve only while the hub holds the connection.
// Every omp run over SSH opens both.
func (f *Fleet) Forwards(ctx context.Context, c *ssh.Client, h *store.Host) (func(), error) {
	return f.ForwardsFor(ctx, c, h, 0)
}

// ForwardsFor is Forwards for a job: what the door serves through the
// sudo path is recorded under that job as well as in the event log.
func (f *Fleet) ForwardsFor(ctx context.Context, c *ssh.Client, h *store.Host, jobID int64) (func(), error) {
	proxyAddr, closeProxy, err := f.vaultProxy(h)
	if err != nil {
		return nil, err
	}
	stopVault, err := remote.Forward(ctx, c, f.VaultAddr, proxyAddr)
	if err != nil {
		closeProxy()
		return nil, err
	}
	doorAddr, closeDoor, err := f.door(c, h, jobID)
	if err != nil {
		stopVault()
		closeProxy()
		return nil, err
	}
	stopDoor, err := remote.Forward(ctx, c, RouterForwardAddr, doorAddr)
	if err != nil {
		stopVault()
		closeProxy()
		closeDoor()
		return nil, err
	}
	return func() { stopVault(); stopDoor(); closeDoor(); closeProxy() }, nil
}

// vaultProxy is the vault as this one host may see it: a loopback
// listener that accepts only the host's own token, swaps in the real one,
// and passes the request to the vault. The token is read from the store
// on every request, so Revoke takes effect on the next call.
func (f *Fleet) vaultProxy(h *store.Host) (string, func(), error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	target := &url.URL{Scheme: "http", Host: f.VaultAddr}
	rp := httputil.NewSingleHostReverseProxy(target)
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur, err := f.Store.Host(r.Context(), strconv.FormatInt(h.ID, 10))
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if err != nil || cur.VaultToken == "" || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(cur.VaultToken)) != 1 {
			f.Notify("vault.refused", h.Name, h.Name+" presented a token the vault does not know")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		real, err := f.VaultToken()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		r.Header.Set("Authorization", "Bearer "+real)
		rp.ServeHTTP(w, r)
	})}
	go func() { _ = srv.Serve(ln) }()
	return ln.Addr().String(), func() { _ = srv.Close() }, nil
}

// door is the one thing a remote can reach on the hub through its own
// tunnel: the router, the secrets this host was granted, and root — one
// command at a time, through the gate, on this same connection. The
// panel's listener is never forwarded, so an agent on a remote has no
// route to the hub's API.
func (f *Fleet) door(c *ssh.Client, h *store.Host, jobID int64) (string, func(), error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	mux := http.NewServeMux()
	if f.Router != nil {
		for _, p := range []string{"/api/tags", "/api/chat", "/api/generate", "/api/embed", "/api/embeddings", "/api/show", "/api/version", "/v1/"} {
			mux.Handle(p, f.Router)
		}
	}
	mux.HandleFunc("GET /secrets/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		v, err := f.Store.ReadSecret(r.Context(), name, h.ID)
		if err != nil {
			f.Notify("secret.refused", h.Name, h.Name+" asked for secret "+name+": not granted")
			http.NotFound(w, r)
			return
		}
		f.Notify("secret.read", h.Name, h.Name+" read secret "+name)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, v)
	})
	mux.HandleFunc("POST /sudo", func(w http.ResponseWriter, r *http.Request) {
		raw, err := base64.StdEncoding.DecodeString(r.Header.Get("X-Command"))
		command := strings.TrimSpace(string(raw))
		if err != nil || command == "" {
			http.Error(w, "usage: homedash-sudo COMMAND [ARGS...]", http.StatusBadRequest)
			return
		}
		if v, _ := f.Store.Setting(r.Context(), "jobs.sudo"); v == "off" {
			f.Notify("sudo.refused", h.Name, h.Name+": homedash-sudo is off in Settings: "+shortCmd(command))
			http.Error(w, "refused: homedash-sudo is switched off on this hub", http.StatusForbidden)
			return
		}
		if err := gate.PrivilegedOn(command, SystemDevices(h)); err != nil {
			f.Notify("sudo.refused", h.Name, h.Name+": "+err.Error())
			f.jobLine(r.Context(), jobID, "sudo", command, -1, err.Error())
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		f.Notify("sudo.run", h.Name, h.Name+": homedash-sudo "+shortCmd(command))
		body := http.MaxBytesReader(w, r.Body, 64<<20)
		res, err := remote.RunOn(r.Context(), c, []string{"sudo", "-n", "sh", "-c", "exec 2>&1; " + command}, body)
		if err != nil {
			f.jobLine(r.Context(), jobID, "sudo", command, -1, err.Error())
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		out := string(res.Stdout) + string(res.Stderr)
		f.jobLine(r.Context(), jobID, "sudo", command, res.ExitCode, tailStr(out))
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Exit-Code", strconv.Itoa(res.ExitCode))
		_, _ = io.WriteString(w, out)
	})
	mux.HandleFunc("/", http.NotFound)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return ln.Addr().String(), func() { _ = srv.Close() }, nil
}

// jobLine records what the door did for a job, as one event line the
// panel and the hub's agent read beside omp's own.
func (f *Fleet) jobLine(ctx context.Context, jobID int64, kind, command string, exit int, text string) {
	if jobID == 0 {
		return
	}
	b, _ := json.Marshal(map[string]any{"type": kind, "command": command, "exit": exit, "text": text})
	_ = f.Store.AppendJobEvent(ctx, jobID, string(b))
}

func shortCmd(s string) string {
	if len(s) > 120 {
		return s[:117] + "..."
	}
	return s
}

func tailStr(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 2000 {
		return "..." + s[len(s)-2000:]
	}
	return s
}

// secretHelper is `homedash-secret NAME` on a remote: the door's URL.
const secretHelper = `#!/bin/sh
# HomeDash: read a secret the hub granted this machine, while a job runs.
[ -n "$1" ] || { echo "usage: homedash-secret NAME" >&2; exit 2; }
exec curl -fsS "http://` + RouterForwardAddr + `/secrets/$1"
`

// sudoHelper is `homedash-sudo COMMAND...` on a remote: not sudo, a
// request to the hub through the door, which checks it, runs it as root
// over its own connection, and returns the output and the exit code.
// Standard input, if any, is the command's.
const sudoHelper = `#!/bin/sh
# HomeDash: run one command as root through the hub, while a job runs.
# The hub checks it against the gate and the privileged allowlist, runs
# it, logs it, and returns the output. There is no other route to root.
[ $# -gt 0 ] || { echo "usage: homedash-sudo COMMAND [ARGS...]" >&2; exit 2; }
out=$(mktemp); hdr=$(mktemp); trap 'rm -f "$out" "$hdr"' EXIT
in=/dev/null; [ -t 0 ] || in=-
status=$(curl -sS -o "$out" -D "$hdr" -w '%{http_code}' -H "X-Command: $(printf '%s' "$*" | base64 -w0)" --data-binary "@$in" "http://` + RouterForwardAddr + `/sudo") || { echo "homedash-sudo: the hub did not answer (is this a job?)" >&2; exit 125; }
cat "$out"
case "$status" in
  200) exit "$(awk 'tolower($1)=="x-exit-code:"{gsub("\r","",$2); print $2}' "$hdr")" ;;
  403) exit 126 ;;
  *) echo "homedash-sudo: hub answered $status" >&2; exit 125 ;;
esac
`

// UpdateCredentials is the card action: a fresh snapshot on one host,
// and the end of any "credentials revoked" state on its card (H-9).
func (f *Fleet) UpdateCredentials(ctx context.Context, h *store.Host) error {
	c, err := f.Exec.Dial(ctx, Target(h))
	if err != nil {
		return err
	}
	defer c.Close()
	if _, err := f.deliverCredentials(ctx, c, h); err != nil {
		f.Notify("agent.credentials_failed", h.Name, h.Name+": credential pull failed: "+err.Error())
		return err
	}
	return f.Store.SetCredentialsRevoked(ctx, h.ID, "")
}

// --- the heartbeat ----------------------------------------------------

// sweepWidth is how many hosts a heartbeat or a scan has in flight at
// once: an unplugged machine costs its own timeout, not everyone's.
const sweepWidth = 8

// each runs fn on every host, sweepWidth at a time, and returns when all
// have finished or ctx has ended.
func each(ctx context.Context, hosts []store.Host, fn func(*store.Host)) {
	sem := make(chan struct{}, sweepWidth)
	var wg sync.WaitGroup
	for i := range hosts {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(h *store.Host) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(h)
		}(&hosts[i])
	}
	wg.Wait()
}

// Heartbeat sweeps every host once a minute until ctx ends, and rolls the
// numbers up once an hour.
func (f *Fleet) Heartbeat(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	lastRollup := time.Now()
	for {
		hosts, err := f.Store.Hosts(ctx)
		if err == nil {
			each(ctx, hosts, func(h *store.Host) { f.Sweep(ctx, h) })
			if ctx.Err() != nil {
				return
			}
			f.markStaleMobile(ctx, hosts)
		}
		if time.Since(lastRollup) > time.Hour {
			if err := f.Store.RollupMetrics(ctx); err != nil {
				f.Log.Error("metrics rollup", "err", err)
			}
			if err := f.Store.RollupUsage(ctx); err != nil {
				f.Log.Error("usage rollup", "err", err)
			}
			lastRollup = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// Sweep is one heartbeat on one host: a real connection with the pinned
// key, fresh facts, and the status those two things imply. A transition
// is an event; a repeat is not.
func (f *Fleet) Sweep(ctx context.Context, h *store.Host) {
	// A phone is never dialed into: it reports in on its own, through
	// SetMobileStatus. Its offline transition is markStaleMobile's clock,
	// not a failed connection here.
	if h.Kind == "mobile" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	status := "online"
	var facts json.RawMessage
	var model string
	r, err := f.Exec.Sh(ctx, Target(h), factsScript)
	switch {
	case errors.Is(err, remote.ErrKeyMismatch):
		status = "mismatch"
	case err != nil:
		status = "offline"
	case r.ExitCode != 0 || !json.Valid(r.Stdout):
		status = "offline"
		f.Log.Warn("facts script failed", "host", h.Name, "exit", r.ExitCode, "stderr", tail(r.Stderr))
	default:
		facts = bytes.TrimSpace(r.Stdout)
		var p Facts
		if json.Unmarshal(facts, &p) == nil {
			if p.Agent != nil {
				model = p.Agent.Model
			}
			mounts := map[string]int64{}
			for _, m := range p.Mounts {
				mounts[m.Path] = m.Free
			}
			mb, _ := json.Marshal(mounts)
			f.checkFullness(ctx, h, p)
			gpu := 0.0
			if p.GPU != nil && p.GPU.Busy {
				gpu = 1
			}
			if err := f.Store.RecordMetric(ctx, h.ID, store.Metric{MemUsed: p.MemUsed, MemTotal: p.MemTotal, Load1: p.Load1, GPUBusy: gpu, Mounts: mb}); err != nil {
				f.Log.Error("record metric", "host", h.Name, "err", err)
			}
		}
	}
	prev, err := f.Store.SetHostStatus(ctx, h.ID, status, facts, model)
	if err != nil {
		f.Log.Error("set host status", "host", h.Name, "err", err)
		return
	}
	if prev != status {
		msg := map[string]string{
			"online":   h.Name + " is online",
			"offline":  h.Name + " went offline",
			"mismatch": h.Name + " presented a different host key than the one enrolled — refused",
		}[status]
		f.Notify("host."+status, h.Name, msg)
	}
	h.Status = status
}

// RemoteModel is the model a remote runs when its card has no override:
// agent.remote_model, or the hub's own agent.default_model while that is
// blank, so one model set in Settings still covers the whole house.
func (f *Fleet) RemoteModel(ctx context.Context) string {
	model, _ := f.Store.Setting(ctx, "agent.remote_model")
	if model == "" {
		model, _ = f.Store.Setting(ctx, "agent.default_model")
	}
	return model
}

// SetAgentModel is the card's Agent override: the model this remote's
// agent is set to, written into its own config.yml. Empty means the
// fleet default. The card keeps showing what the heartbeat reads back.
func (f *Fleet) SetAgentModel(ctx context.Context, h *store.Host, model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		model = f.RemoteModel(ctx)
	}
	content := "modelRoles:\n  default: " + model + "\n"
	if model == "" {
		content = ""
	}
	path := "/home/" + gate.AgentAccount + "/.omp/agent/config.yml"
	if err := f.WriteFile(ctx, h, path, []byte(content), "0600", true); err != nil {
		return err
	}
	owner := gate.AgentAccount + ":" + gate.AgentAccount
	if _, err := f.Exec.Run(ctx, Target(h), []string{"sudo", "-n", "chown", owner, path}, nil); err != nil {
		return err
	}
	_ = f.Store.AppendRebuildScript(ctx, h.ID, "# HomeDash: agent model (set from the panel)\nprintf 'modelRoles:\\n  default: %s\\n' "+shq(model)+" > "+path+"; chown "+owner+" "+path)
	go f.Sweep(context.Background(), h)
	return nil
}

func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// --- the gated door ---------------------------------------------------

// Run executes one command line on a host as the hub's account, after
// the gate. Every caller — panel, window, assistant, task — comes here.
// With forwards, the command runs the way a job does: the vault and the
// job door on the remote's loopback for its duration.
func (f *Fleet) Run(ctx context.Context, h *store.Host, command string, timeout time.Duration, forwards bool) (*remote.Result, error) {
	if err := gate.CheckOn(command, SystemDevices(h)); err != nil {
		f.Notify("gate.refused", h.Name, h.Name+": "+err.Error())
		return nil, err
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if !forwards {
		return f.Exec.Run(ctx, Target(h), []string{"sh", "-lc", command}, nil)
	}
	c, err := f.Exec.Dial(ctx, Target(h))
	if err != nil {
		return nil, err
	}
	defer c.Close()
	stop, err := f.Forwards(ctx, c, h)
	if err != nil {
		return nil, err
	}
	defer stop()
	return remote.RunOn(ctx, c, []string{"sh", "-lc", command}, nil)
}

// WriteFile puts a file on a host. Paths under /etc/ssh are the gate's.
func (f *Fleet) WriteFile(ctx context.Context, h *store.Host, path string, content []byte, mode string, sudo bool) error {
	if strings.HasPrefix(path, "/etc/ssh/") || strings.HasSuffix(path, "authorized_keys") {
		err := &gate.Refusal{Reason: "editing SSH config by hand: " + path}
		f.Notify("gate.refused", h.Name, h.Name+": "+err.Error())
		return err
	}
	c, err := f.Exec.Dial(ctx, Target(h))
	if err != nil {
		return err
	}
	defer c.Close()
	return remote.WriteFile(ctx, c, path, content, mode, sudo)
}

// --- the lock ---------------------------------------------------------

// lockConfig is the sshd drop-in that locks a machine to the hub's
// account. sshd reads drop-ins first, so its values win.
const lockConfig = `# Written by HomeDash. The lock: only the hub's account may log in.
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
AllowUsers homedash
`

// SetLock turns direct SSH access on a host off (locked) or on. Locking
// verifies what the candidate config would actually allow — for the hub's
// account and for everyone else — before it takes effect; either check
// failing leaves the machine unchanged.
func (f *Fleet) SetLock(ctx context.Context, h *store.Host, locked bool) error {
	c, err := f.Exec.Dial(ctx, Target(h))
	if err != nil {
		return err
	}
	defer c.Close()
	const path = "/etc/ssh/sshd_config.d/00-homedash-lock.conf"
	if !locked {
		r, err := remote.RunOn(ctx, c, []string{"sudo", "-n", "sh", "-c", "rm -f " + path + " && (systemctl reload ssh || systemctl reload sshd)"}, nil)
		if err != nil {
			return err
		}
		if r.ExitCode != 0 {
			return fmt.Errorf("unlock: %s", tail(r.Stderr))
		}
		return nil
	}
	return f.applyLock(ctx, c, path)
}

func (f *Fleet) applyLock(ctx context.Context, c *ssh.Client, path string) error {
	if err := remote.WriteFile(ctx, c, path+".candidate", []byte(lockConfig), "0644", true); err != nil {
		return err
	}
	script := `set -e
p="$1"
check() { sshd -T -C user="$1",host=x,addr=127.0.0.1 2>/dev/null; }
mv -f "$p.candidate" "$p"
hub=$(check homedash | awk '$1=="allowusers"{u=$2} $1=="pubkeyauthentication"{k=$2} END{print u" "k}')
other=$(check nobody | awk '$1=="passwordauthentication"{a=$2} $1=="permitrootlogin"{r=$2} END{print a" "r}')
if [ "$hub" != "homedash yes" ]; then rm -f "$p"; echo "the hub's account would be refused ($hub)" >&2; exit 3; fi
if [ "$other" != "no no" ]; then rm -f "$p"; echo "others would still be allowed ($other)" >&2; exit 4; fi
sshd -t
systemctl reload ssh 2>/dev/null || systemctl reload sshd
`
	r, err := remote.RunOn(ctx, c, []string{"sudo", "-n", "sh", "-s", "--", path}, strings.NewReader(script))
	if err != nil {
		return err
	}
	if r.ExitCode != 0 {
		return fmt.Errorf("lock refused, machine unchanged: %s", tail(r.Stderr))
	}
	return nil
}

// DiskPercent is the fullness threshold from Settings (notify.disk_percent,
// 90 by default): the one number a remote's mount and the hub's own state
// disk are both held to.
func (f *Fleet) DiskPercent(ctx context.Context) int {
	pct := 90
	if v, _ := f.Store.Setting(ctx, "notify.disk_percent"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 && n < 100 {
			pct = n
		}
	}
	return pct
}

// checkFullness records the transition of a mountpoint across the
// threshold in Settings (notify.disk_percent, 90 by default), in either
// direction, once.
func (f *Fleet) checkFullness(ctx context.Context, h *store.Host, p Facts) {
	pct := f.DiskPercent(ctx)
	f.fullMu.Lock()
	defer f.fullMu.Unlock()
	if f.full == nil {
		f.full = map[string]bool{}
	}
	for _, m := range p.Mounts {
		if m.Size == 0 || strings.HasPrefix(m.Path, "/mnt/homedash/") {
			continue
		}
		used := int(100 - m.Free*100/m.Size)
		key := h.Name + ":" + m.Path
		above := used >= pct
		if was, seen := f.full[key]; seen && was != above || !seen && above {
			if above {
				f.Notify("disk.full", h.Name, fmt.Sprintf("%s: %s is %d%% full", h.Name, m.Path, used))
			} else {
				f.Notify("disk.ok", h.Name, fmt.Sprintf("%s: %s is back to %d%%", h.Name, m.Path, used))
			}
		}
		f.full[key] = above
	}
}

func tail(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.LastIndex(s, "\n"); i >= 0 && len(s)-i > 1 {
		s = s[i+1:]
	}
	if len(s) > 300 {
		s = s[len(s)-300:]
	}
	return s
}

func randomCode() string {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

// FitResult is what the Models tab shows for one machine: what llmfit
// found and the models with an Ollama name that fit, best first.
type FitResult struct {
	System json.RawMessage `json:"system"`
	Models []FitModel      `json:"models"`
}

// FitModel is one recommendation that can be pulled.
type FitModel struct {
	Ollama       string   `json:"ollama"`
	Name         string   `json:"name"`
	Fit          string   `json:"fit"`
	Score        float64  `json:"score"`
	TokensPerSec float64  `json:"tokensPerSec"`
	MemoryGB     float64  `json:"memoryGB"`
	DiskGB       float64  `json:"diskGB"`
	Capabilities []string `json:"capabilities"`
	UseCase      string   `json:"useCase,omitempty"`
}

// Fit asks llmfit on the host what runs well there. llmfit ranks
// everything it knows; only the entries with an Ollama name can be
// pulled from the grid, so those are kept, deduplicated, best first.
func (f *Fleet) Fit(ctx context.Context, h *store.Host, n int) (*FitResult, error) {
	if n <= 0 || n > 25 {
		n = 8
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	r, err := f.Exec.Run(ctx, Target(h), []string{"sh", "-lc", "llmfit recommend --json -n 400 --no-dashboard"}, nil)
	if err != nil {
		return nil, err
	}
	if r.ExitCode != 0 || !json.Valid(r.Stdout) {
		return nil, fmt.Errorf("llmfit on %s: %s", h.Name, tail(r.Stderr))
	}
	var raw struct {
		System json.RawMessage `json:"system"`
		Models []struct {
			Name         string   `json:"name"`
			Ollama       *string  `json:"ollama_name"`
			Fit          string   `json:"fit_level"`
			Score        float64  `json:"score"`
			TPS          *float64 `json:"estimated_tps"`
			Mem          float64  `json:"memory_required_gb"`
			Disk         float64  `json:"disk_size_gb"`
			Capabilities []string `json:"capabilities"`
			UseCase      string   `json:"use_case"`
		} `json:"models"`
	}
	if err := json.Unmarshal(r.Stdout, &raw); err != nil {
		return nil, err
	}
	out := &FitResult{System: raw.System, Models: []FitModel{}}
	seen := map[string]bool{}
	for _, m := range raw.Models {
		if m.Ollama == nil || *m.Ollama == "" || seen[*m.Ollama] {
			continue
		}
		seen[*m.Ollama] = true
		fm := FitModel{Ollama: *m.Ollama, Name: m.Name, Fit: m.Fit, Score: m.Score, MemoryGB: m.Mem, DiskGB: m.Disk, Capabilities: m.Capabilities, UseCase: m.UseCase}
		if m.TPS != nil {
			fm.TokensPerSec = *m.TPS
		}
		out.Models = append(out.Models, fm)
		if len(out.Models) == n {
			break
		}
	}
	return out, nil
}
