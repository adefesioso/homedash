package server

// GET /api/hub/health: the hub about its own well-being. The hub is read
// the way a remote is — what the machine reports about itself right now,
// from /proc and the state directory, never what anyone asked for — and
// then one line per thing the hub has to keep running for the lab to
// work. The Health tab draws it; the rail's LED is its worst line.

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/adefesioso/homedash/hub/internal/backup"
)

// A check's state: ok, warn (needs a look), bad (something is not
// working), off (not configured, so nothing to judge).
const (
	healthOK   = "ok"
	healthWarn = "warn"
	healthBad  = "bad"
	healthOff  = "off"
)

type healthCheck struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Detail string `json:"detail"`
}

type hubMachine struct {
	Hostname string  `json:"hostname"`
	OS       string  `json:"os"`
	Kernel   string  `json:"kernel"`
	Arch     string  `json:"arch"`
	Cores    int     `json:"cores"`
	Load1    float64 `json:"load1"`
	MemTotal uint64  `json:"memTotal"`
	MemUsed  uint64  `json:"memUsed"`
	Uptime   int64   `json:"uptime"`
}

type hubProcess struct {
	Version    string `json:"version"`
	Started    string `json:"started"`
	RSS        uint64 `json:"rss"`
	Goroutines int    `json:"goroutines"`
	Sessions   int    `json:"sessions"`
}

type hubStateDir struct {
	Path     string `json:"path"`
	DBSize   int64  `json:"dbSize"`
	DiskSize uint64 `json:"diskSize"`
	DiskFree uint64 `json:"diskFree"`
}

type hubHealth struct {
	State    string        `json:"state"`
	At       string        `json:"at"`
	Machine  hubMachine    `json:"machine"`
	Process  hubProcess    `json:"process"`
	StateDir hubStateDir   `json:"stateDir"`
	Checks   []healthCheck `json:"checks"`
}

func (s *Server) hubHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	writeJSON(w, s.healthReport(ctx))
}

func (s *Server) healthReport(ctx context.Context) hubHealth {
	h := hubHealth{At: time.Now().UTC().Format(time.RFC3339)}
	h.Machine = readMachine()
	h.Process = hubProcess{Version: s.Version, Started: s.Started.UTC().Format(time.RFC3339), RSS: readRSS(), Goroutines: runtime.NumGoroutine(), Sessions: s.Agent.Status().LiveWindows}
	h.StateDir = readStateDir(s.Store.Path)

	m := h.Machine
	if m.MemTotal > 0 {
		pct := int(m.MemUsed * 100 / m.MemTotal)
		h.add("Memory", pctState(pct, 90, 0), fmt.Sprintf("%d%% of %s in use", pct, gb(m.MemTotal)))
	}
	if m.Cores > 0 {
		st := healthOK
		if m.Load1 > float64(m.Cores) {
			st = healthWarn
		}
		h.add("Load", st, fmt.Sprintf("%.2f on %d cores", m.Load1, m.Cores))
	}
	if d := h.StateDir; d.DiskSize > 0 {
		pct := int((d.DiskSize - d.DiskFree) * 100 / d.DiskSize)
		limit := s.Fleet.DiskPercent(ctx)
		h.add("State disk", pctState(pct, limit, limit-10), fmt.Sprintf("%d%% full, %s free of %s for %s", pct, gb(d.DiskFree), gb(d.DiskSize), d.Path))
	}

	ag := s.Agent.Status()
	switch {
	case ag.Ready:
		h.add("Agent", healthOK, "omp "+ag.OmpVersion+" installed")
	case ag.InstallError != "":
		h.add("Agent", healthBad, "omp not installed: "+ag.InstallError)
	default:
		h.add("Agent", healthWarn, "omp "+ag.OmpVersion+" still being fetched")
	}
	switch {
	case ag.VaultRunning:
		h.add("Credential vault", healthOK, "running")
	case !ag.Ready:
		h.add("Credential vault", healthWarn, "waiting for omp")
	default:
		h.add("Credential vault", healthBad, "not running; the hub restarts it every few seconds")
	}

	h.add(backupCheck(s.Backup.Status(ctx)))

	if hosts, err := s.Store.Hosts(ctx); err != nil {
		h.add("Hosts", healthBad, "the state file could not be read: "+err.Error())
	} else if len(hosts) == 0 {
		h.add("Hosts", healthOff, "none enrolled")
	} else {
		online, mismatch := 0, 0
		for _, x := range hosts {
			switch x.Status {
			case "online":
				online++
			case "mismatch":
				mismatch++
			}
		}
		switch {
		case mismatch > 0:
			h.add("Hosts", healthBad, fmt.Sprintf("%d of %d online, %d with a host key that changed", online, len(hosts), mismatch))
		case online < len(hosts):
			h.add("Hosts", healthWarn, fmt.Sprintf("%d of %d online", online, len(hosts)))
		default:
			h.add("Hosts", healthOK, fmt.Sprintf("%d of %d online", online, len(hosts)))
		}
	}

	if tasks, err := s.Store.Tasks(ctx); err != nil {
		h.add("Tasks", healthBad, "the state file could not be read: "+err.Error())
	} else {
		enabled, failing := 0, 0
		for _, t := range tasks {
			if t.Enabled {
				enabled++
				if t.Failing {
					failing++
				}
			}
		}
		switch {
		case enabled == 0:
			h.add("Tasks", healthOff, "none scheduled")
		case failing > 0:
			h.add("Tasks", healthWarn, fmt.Sprintf("%d of %d failing", failing, enabled))
		default:
			h.add("Tasks", healthOK, fmt.Sprintf("%d scheduled, none failing", enabled))
		}
	}

	ps := s.Peers.GetStatus(ctx)
	switch {
	case !ps.Enabled:
		h.add("Space", healthOff, "not joined")
	case ps.Connected > 0:
		h.add("Space", healthOK, fmt.Sprintf("%d of %d peers connected in %s", ps.Connected, ps.Found, ps.Space))
	case ps.Found > 0:
		h.add("Space", healthWarn, fmt.Sprintf("%d peers found in %s, none connected", ps.Found, ps.Space))
	default:
		h.add("Space", healthOK, fmt.Sprintf("in %s, no peers found yet", ps.Space))
	}

	h.State = healthOK
	for _, c := range h.Checks {
		if c.State == healthBad || (c.State == healthWarn && h.State != healthBad) {
			h.State = c.State
		}
	}
	return h
}

func (h *hubHealth) add(name, state, detail string) {
	h.Checks = append(h.Checks, healthCheck{Name: name, State: state, Detail: detail})
}

// pctState grades a fullness: bad at or past limit, warn at or past soft
// (0: no warning band), ok below.
func pctState(pct, limit, soft int) string {
	switch {
	case pct >= limit:
		return healthBad
	case soft > 0 && pct >= soft:
		return healthWarn
	}
	return healthOK
}

// backupCheck is one line for the export: none yet is warn (the state
// directory lives on this disk only), one older than thirty days is
// warn, a recent one is ok.
func backupCheck(b backup.Status) (string, string, string) {
	last, err := time.Parse(time.RFC3339, b.Last)
	if err != nil {
		return "Export", healthWarn, "never exported; the state directory lives on this disk only"
	}
	age := time.Since(last)
	detail := "last export " + ageWords(age) + " ago"
	if age > 30*24*time.Hour {
		return "Export", healthWarn, detail + " — make a fresh one"
	}
	return "Export", healthOK, detail
}

func ageWords(d time.Duration) string {
	switch {
	case d < 90*time.Second:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < 90*time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func gb(b uint64) string {
	if b >= 1<<30 {
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	}
	return fmt.Sprintf("%d MB", b>>20)
}

// readMachine is facts.sh for the hub itself: the same fields a remote
// reports, read from /proc instead of piped over SSH. A field that
// cannot be read is left at zero rather than failing the page.
func readMachine() hubMachine {
	m := hubMachine{Arch: runtime.GOARCH, Cores: runtime.NumCPU()}
	m.Hostname, _ = os.Hostname()
	m.OS = osRelease()
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		m.Kernel = strings.TrimSpace(string(b))
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			m.Load1, _ = strconv.ParseFloat(f[0], 64)
		}
	}
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			up, _ := strconv.ParseFloat(f[0], 64)
			m.Uptime = int64(up)
		}
	}
	if f, err := os.Open("/proc/meminfo"); err == nil {
		defer f.Close()
		var total, avail uint64
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			k, v := procKV(sc.Text())
			switch k {
			case "MemTotal":
				total = v
			case "MemAvailable":
				avail = v
			}
		}
		if total > 0 {
			m.MemTotal, m.MemUsed = total, total-avail
		}
	}
	return m
}

// readRSS is the hub process's resident memory, from /proc/self/status.
func readRSS() uint64 {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v := procKV(sc.Text()); k == "VmRSS" {
			return v
		}
	}
	return 0
}

// procKV reads one "Key:  1234 kB" line into bytes.
func procKV(line string) (string, uint64) {
	k, rest, ok := strings.Cut(line, ":")
	if !ok {
		return "", 0
	}
	f := strings.Fields(rest)
	if len(f) == 0 {
		return k, 0
	}
	n, _ := strconv.ParseUint(f[0], 10, 64)
	if len(f) > 1 && f[1] == "kB" {
		n <<= 10
	}
	return k, n
}

func osRelease() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

// readStateDir sizes the database (with its WAL and shm beside it) and
// the filesystem it sits on.
func readStateDir(dbPath string) hubStateDir {
	d := hubStateDir{Path: filepath.Dir(dbPath)}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if fi, err := os.Stat(dbPath + suffix); err == nil {
			d.DBSize += fi.Size()
		}
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(d.Path, &st); err == nil {
		d.DiskSize = st.Blocks * uint64(st.Bsize)
		d.DiskFree = st.Bavail * uint64(st.Bsize)
	}
	return d
}
