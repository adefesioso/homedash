// Package apps is the compose stacks on the remotes. Nothing about an
// installed app is stored on the hub: the tab asks every remote what it
// has, live, on each open, so there is no state to drift. A stack lives
// at ~/stacks/<name>/compose.yml under the hub's account, which is also
// where a stack someone deployed by hand simply appears.
package apps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/adefesioso/homedash/hub/internal/fleet"
	"github.com/adefesioso/homedash/hub/internal/gate"
	"github.com/adefesioso/homedash/hub/internal/store"
)

// Needs is the rough requirements line placement scores against.
type Needs struct {
	Cores    int  `json:"cores"`
	MemoryMB int  `json:"memoryMB"`
	DiskGB   int  `json:"diskGB"`
	GPU      bool `json:"gpu"`
	// Cluster names a storage cluster the stack wants; placement then
	// belongs on that cluster's gateway.
	Cluster string `json:"cluster,omitempty"`
}

// Entry is one catalog item: a compose file with a requirements line.
type Entry struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Needs       Needs    `json:"needs"`
	Volumes     []string `json:"volumes"`
	Compose     string   `json:"compose"`
	// Files are plain-text files the compose's bind mounts expect to
	// exist, written under the stack directory before it comes up.
	Files []SetupFile `json:"files,omitempty"`
	// EnvTemplate is a default .env carried into the install dialog
	// instead of starting blank.
	EnvTemplate string `json:"envTemplate,omitempty"`
	// SetupCommands run once, in the stack directory, before the stack
	// comes up (e.g. "mkdir -p data"). They meet the same gate every
	// command on a host meets (Fleet.Run) — not a sandbox, and not
	// softened for a human the way ComposeRefusal is.
	SetupCommands []string `json:"setupCommands,omitempty"`
}

// SetupFile is one plain-text file a catalog entry expects to exist —
// an nginx.conf a bind mount points at, e.g. Path is relative to the
// stack's directory.
type SetupFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// safeRelPath rejects an absolute path or one that climbs out of the
// stack directory with a ".." segment.
func safeRelPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == ".." {
			return false
		}
	}
	return true
}

// ErrCatalogNotFound is DeleteCatalogEntry's answer to a name that was
// never added.
var ErrCatalogNotFound = errors.New("no catalog entry by that name")

// Offline is Deploy's and Action's answer when the host's last known
// status isn't online: refused before a dial is even attempted, so the
// message is this clean one rather than a raw connect timeout (A-4).
type Offline struct{ Host string }

func (e *Offline) Error() string { return e.Host + " is offline" }

// Apps owns the stacks and the catalog.
type Apps struct {
	Store  *store.Store
	Fleet  *fleet.Fleet
	Notify fleet.Notify
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Catalog is every entry you've added, by name.
func (a *Apps) Catalog(ctx context.Context) ([]Entry, error) {
	custom, err := a.Store.CatalogEntries(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(custom))
	for _, c := range custom {
		var e Entry
		if json.Unmarshal([]byte(c), &e) == nil && e.Name != "" {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// AddCatalogEntry stores an entry, replacing any of the same name.
// Nothing about it is privileged: what lands on the remote is a compose
// file.
func (a *Apps) AddCatalogEntry(ctx context.Context, e Entry) error {
	if !nameRe.MatchString(e.Name) {
		return errors.New("a catalog name is lowercase letters, digits, - and _")
	}
	if strings.TrimSpace(e.Compose) == "" {
		return errors.New("an entry needs a compose file")
	}
	for _, f := range e.Files {
		if !safeRelPath(f.Path) {
			return fmt.Errorf("setup file path %q escapes the stack directory", f.Path)
		}
	}
	b, _ := json.Marshal(e)
	return a.Store.SetCatalogEntry(ctx, e.Name, string(b))
}

// DeleteCatalogEntry removes an entry. A name that was never added comes
// back ErrCatalogNotFound.
func (a *Apps) DeleteCatalogEntry(ctx context.Context, name string) error {
	custom, err := a.Store.CatalogEntries(ctx)
	if err != nil {
		return err
	}
	for _, c := range custom {
		var e Entry
		if json.Unmarshal([]byte(c), &e) == nil && e.Name == name {
			return a.Store.DeleteCatalogEntry(ctx, name)
		}
	}
	return ErrCatalogNotFound
}

// --- what a remote has ------------------------------------------------

// Stack is one compose project on one host, as docker reports it.
type Stack struct {
	Host       string      `json:"host"`
	Name       string      `json:"name"`
	Status     string      `json:"status"`
	ConfigFile string      `json:"configFile"`
	Containers []Container `json:"containers"`
	Managed    bool        `json:"managed"` // under ~/stacks, so the hub can read and update its file
}

// Container is one service's state.
type Container struct {
	Name   string `json:"name"`
	Image  string `json:"image"`
	State  string `json:"state"`
	Status string `json:"status"`
	Ports  string `json:"ports"`
}

// List asks every online remote for its stacks and their containers.
func (a *Apps) List(ctx context.Context) ([]Stack, []string, error) {
	hosts, err := a.Store.Hosts(ctx)
	if err != nil {
		return nil, nil, err
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var out []Stack
	var errs []string
	for i := range hosts {
		h := &hosts[i]
		if h.Status != "online" {
			// Say which remote didn't answer rather than just leaving its
			// stacks out of the list (D-1): Apps.svelte already renders
			// this the same way it renders a listOn failure below.
			errs = append(errs, h.Name+": "+h.Status)
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ss, err := a.listOn(ctx, h)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, h.Name+": "+err.Error())
				return
			}
			out = append(out, ss...)
		}()
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		return out[i].Name < out[j].Name
	})
	return out, errs, nil
}

func (a *Apps) listOn(ctx context.Context, h *store.Host) ([]Stack, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// One round trip: every project, then every container with its project label.
	script := `docker compose ls --all --format json; echo; docker ps -a --format '{{json .}}' --filter label=com.docker.compose.project`
	r, err := a.Fleet.Exec.Run(ctx, fleet.Target(h), []string{"sh", "-lc", script}, nil)
	if err != nil {
		return nil, err
	}
	if r.ExitCode != 0 {
		return nil, fmt.Errorf("docker: %s", lastLine(r.Stderr))
	}
	parts := strings.SplitN(string(r.Stdout), "\n\n", 2)
	var projects []struct {
		Name        string `json:"Name"`
		Status      string `json:"Status"`
		ConfigFiles string `json:"ConfigFiles"`
	}
	_ = json.Unmarshal([]byte(strings.TrimSpace(parts[0])), &projects)
	byName := map[string]*Stack{}
	var out []Stack
	for _, p := range projects {
		s := Stack{Host: h.Name, Name: p.Name, Status: p.Status, ConfigFile: p.ConfigFiles, Containers: []Container{},
			Managed: strings.Contains(p.ConfigFiles, "/stacks/"+p.Name+"/")}
		out = append(out, s)
	}
	for i := range out {
		byName[out[i].Name] = &out[i]
	}
	if len(parts) == 2 {
		for _, line := range strings.Split(parts[1], "\n") {
			var c struct {
				Names, Image, State, Status, Ports, Labels string
			}
			if json.Unmarshal([]byte(line), &c) != nil {
				continue
			}
			proj := ""
			for _, l := range strings.Split(c.Labels, ",") {
				if strings.HasPrefix(l, "com.docker.compose.project=") {
					proj = strings.TrimPrefix(l, "com.docker.compose.project=")
				}
			}
			if s, ok := byName[proj]; ok {
				s.Containers = append(s.Containers, Container{Name: c.Names, Image: c.Image, State: c.State, Status: c.Status, Ports: c.Ports})
			}
		}
	}
	return out, nil
}

// --- acting on a stack ------------------------------------------------

func stackDir(name string) string { return "stacks/" + name }

// shellDir is the directory part of a setup file's relative path, "."
// when it has none.
func shellDir(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i]
	}
	return "."
}

// Deploy writes the compose file (and an .env if given) under ~/stacks
// and brings the stack up; deploying a name already running on the host
// replaces it, the same as Update. agentCaller is true for a job token,
// a window or an outside assistant through MCP — never for the panel or
// the CLI — and is what decides the compose gate below.
//
// files and setupCommands come from a catalog entry: files are written
// under the stack directory, and setupCommands run there, both before
// the compose file lands and the stack comes up. A setup command meets
// the same gate every command on a host meets (Fleet.Run), regardless
// of agentCaller — unlike the compose gate, this isn't softened for a
// human, since a pasted compose stanza is something a person reading it
// can catch and arbitrary shell is not. A failed setup step stops the
// deploy before anything is brought up, so there is nothing to tear
// down; setup commands are expected to be idempotent, since an earlier
// one in the list may already have run.
func (a *Apps) Deploy(ctx context.Context, h *store.Host, name, compose, env string, files []SetupFile, setupCommands []string, agentCaller bool) (out, warning string, err error) {
	if h.Status != "online" {
		return "", "", &Offline{Host: h.Name}
	}
	if !nameRe.MatchString(name) {
		return "", "", errors.New("a stack name is lowercase letters, digits, - and _")
	}
	if strings.TrimSpace(compose) == "" {
		return "", "", errors.New("a stack needs a compose file")
	}
	for _, f := range files {
		if !safeRelPath(f.Path) {
			return "", "", fmt.Errorf("setup file path %q escapes the stack directory", f.Path)
		}
	}
	if len(files) > 0 || len(setupCommands) > 0 {
		if _, err := a.Fleet.Run(ctx, h, "mkdir -p "+stackDir(name), 30*time.Second, false); err != nil {
			return "", "", err
		}
	}
	for _, f := range files {
		if err := a.Fleet.WriteFile(ctx, h, stackDir(name)+"/"+f.Path, []byte(f.Content), "0644", false); err != nil {
			return "", "", err
		}
	}
	for _, cmd := range setupCommands {
		full := "cd " + stackDir(name) + " && " + cmd
		r, err := a.Fleet.Run(ctx, h, full, 5*time.Minute, false)
		if err != nil {
			out := ""
			if r != nil {
				out = string(r.Stdout) + string(r.Stderr)
			}
			return out, "", fmt.Errorf("setup command failed: %s: %w", cmd, err)
		}
	}
	// The compose gate (A-1): a docker danger list read from the compose
	// file's content. An
	// agent is refused outright; a person deploying from the panel or
	// the CLI is warned but not stopped — see docs/running/safety.md.
	if reason := gate.ComposeRefusal(compose); reason != "" {
		if agentCaller {
			return "", "", &gate.Refusal{Reason: reason}
		}
		warning = "would be refused from an agent: " + reason
	}
	if err := a.Fleet.WriteFile(ctx, h, stackDir(name)+"/compose.yml", []byte(compose), "0644", false); err != nil {
		return "", warning, err
	}
	if env != "" {
		if err := a.Fleet.WriteFile(ctx, h, stackDir(name)+"/.env", []byte(env), "0600", false); err != nil {
			return "", warning, err
		}
	}
	out, err = a.compose(ctx, h, name, "up -d --remove-orphans", 15*time.Minute)
	if err != nil {
		// A failed deploy is removed, not left half up (A-1): the cleanup's
		// own error is dropped, since the one that matters is up -d's.
		_, _ = a.compose(ctx, h, name, "down --remove-orphans", 2*time.Minute)
		return out, warning, err
	}
	dir := "/home/homedash/stacks/" + name
	script := "# HomeDash: stack " + name + " (deployed from the panel)\nmkdir -p " + dir + "\n"
	for i, f := range files {
		marker := fmt.Sprintf("HOMEDASH_FILE_%d", i)
		script += "mkdir -p " + dir + "/" + shellDir(f.Path) + "\ncat > " + dir + "/" + f.Path + " <<'" + marker + "'\n" + strings.TrimRight(f.Content, "\n") + "\n" + marker + "\n"
	}
	for _, cmd := range setupCommands {
		script += "(cd " + dir + " && " + cmd + ")\n"
	}
	script += "cat > " + dir + "/compose.yml <<'HOMEDASH_COMPOSE'\n" + strings.TrimRight(compose, "\n") + "\nHOMEDASH_COMPOSE\n"
	if env != "" {
		script += "cat > " + dir + "/.env <<'HOMEDASH_ENV'\n" + strings.TrimRight(env, "\n") + "\nHOMEDASH_ENV\nchmod 600 " + dir + "/.env\n"
	}
	script += "chown -R homedash:homedash " + dir + "\nsudo -u homedash docker compose -f " + dir + "/compose.yml -p " + name + " up -d"
	_ = a.Store.AppendRebuildScript(ctx, h.ID, script)
	a.Notify("app.deployed", h.Name, name+" deployed on "+h.Name)
	return out, warning, nil
}

// Action is start, stop, restart, pull or remove (with or without volumes).
func (a *Apps) Action(ctx context.Context, h *store.Host, name, action string, volumes bool) (string, error) {
	if h.Status != "online" {
		return "", &Offline{Host: h.Name}
	}
	if !nameRe.MatchString(name) {
		return "", errors.New("bad stack name")
	}
	if action == "pull" {
		// Two calls through the same compose() wrapper, not a shell "&&":
		// a shell "&&" loses compose()'s own "-f" handling for the second
		// half, which is why pull used to succeed and the up -d after it
		// didn't find its file (A-3).
		out, err := a.compose(ctx, h, name, "pull", 15*time.Minute)
		if err != nil {
			return out, err
		}
		up, err := a.compose(ctx, h, name, "up -d", 15*time.Minute)
		return out + up, err
	}
	var args string
	switch action {
	case "start", "stop", "restart":
		args = action
	case "remove":
		args = "down --remove-orphans"
		if volumes {
			args += " --volumes"
		}
	default:
		return "", errors.New("unknown action " + action)
	}
	out, err := a.compose(ctx, h, name, args, 15*time.Minute)
	if err != nil {
		return out, err
	}
	if action == "remove" {
		_, _ = a.Fleet.Exec.Run(ctx, fleet.Target(h), []string{"rm", "-rf", "--", stackDir(name)}, nil)
		_ = a.Store.AppendRebuildScript(ctx, h.ID, "# HomeDash: stack "+name+" removed (from the panel)\nsudo -u homedash docker compose -p "+name+" down --remove-orphans"+map[bool]string{true: " --volumes", false: ""}[volumes]+" 2>/dev/null || true; rm -rf /home/homedash/stacks/"+name)
		a.Notify("app.removed", h.Name, name+" removed from "+h.Name)
	}
	return out, nil
}

// compose runs docker compose for a project. A managed stack is
// addressed by its file; one deployed by hand by its project name only.
func (a *Apps) compose(ctx context.Context, h *store.Host, name, args string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := "cd && if [ -f " + stackDir(name) + "/compose.yml ]; then docker compose -f " + stackDir(name) + "/compose.yml -p " + name + " " + args + "; else docker compose -p " + name + " " + args + "; fi"
	r, err := a.Fleet.Exec.Run(ctx, fleet.Target(h), []string{"sh", "-lc", cmd}, nil)
	if err != nil {
		return "", err
	}
	out := string(r.Stdout) + string(r.Stderr)
	if r.ExitCode != 0 {
		// out already carries the full stdout+stderr, ending in the same
		// line a caller would otherwise repeat by adding lastLine(stderr)
		// to the error text — one copy of the failure, not two (A-1).
		return out, fmt.Errorf("docker compose %s failed", strings.Fields(args)[0])
	}
	return out, nil
}

// File reads a managed stack's compose file and .env.
func (a *Apps) File(ctx context.Context, h *store.Host, name string) (compose, env string, err error) {
	if !nameRe.MatchString(name) {
		return "", "", errors.New("bad stack name")
	}
	r, err := a.Fleet.Exec.Run(ctx, fleet.Target(h), []string{"sh", "-lc", "cd && cat " + stackDir(name) + "/compose.yml; printf '\\n\\x00'; cat " + stackDir(name) + "/.env 2>/dev/null"}, nil)
	if err != nil {
		return "", "", err
	}
	parts := strings.SplitN(string(r.Stdout), "\n\x00", 2)
	if len(parts) == 2 {
		env = parts[1]
	}
	return parts[0], env, nil
}

// Logs is the recent output of a stack's containers.
func (a *Apps) Logs(ctx context.Context, h *store.Host, name string, lines int) (string, error) {
	if lines <= 0 || lines > 5000 {
		lines = 200
	}
	out, err := a.compose(ctx, h, name, fmt.Sprintf("logs --no-color --tail %d", lines), time.Minute)
	if err != nil {
		return out, err
	}
	return out, nil
}

func lastLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	return s
}
