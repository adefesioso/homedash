package fleet

import (
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/adefesioso/homedash/hub/internal/gate"
	"github.com/adefesioso/homedash/hub/internal/remote"
	"github.com/adefesioso/homedash/hub/internal/store"
)

// held is the one SSH client the hub keeps per host for traffic that
// would otherwise dial on every use: a published service's connections.
type held struct {
	mu      sync.Mutex
	clients map[int64]*ssh.Client
}

// Hold returns a kept connection to h, dialing one if there is none or
// the kept one has died. The caller does not close it; Drop does.
func (f *Fleet) Hold(ctx context.Context, h *store.Host) (*ssh.Client, error) {
	f.held.mu.Lock()
	defer f.held.mu.Unlock()
	if f.held.clients == nil {
		f.held.clients = map[int64]*ssh.Client{}
	}
	if c, ok := f.held.clients[h.ID]; ok {
		// A dead connection fails its keepalive; a live one answers.
		if _, _, err := c.SendRequest("keepalive@homedash", true, nil); err == nil {
			return c, nil
		}
		c.Close()
		delete(f.held.clients, h.ID)
	}
	c, err := f.Exec.Dial(ctx, Target(h))
	if err != nil {
		return nil, err
	}
	f.held.clients[h.ID] = c
	return c, nil
}

// Drop closes the kept connection to a host, if any: on removal, and
// when a copy through it fails.
func (f *Fleet) Drop(hostID int64) {
	f.held.mu.Lock()
	defer f.held.mu.Unlock()
	if c, ok := f.held.clients[hostID]; ok {
		c.Close()
		delete(f.held.clients, hostID)
	}
}

// Port opens a direct-tcpip channel on the held connection to a port on
// the remote's loopback: a published service's origin side. Nothing on
// the remote listens for the hub, and a locked host stays locked.
func (f *Fleet) Port(ctx context.Context, h *store.Host, port int) (net.Conn, error) {
	c, err := f.Hold(ctx, h)
	if err != nil {
		return nil, err
	}
	conn, err := c.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		f.Drop(h.ID)
		return nil, err
	}
	return conn, nil
}

// --- the hub's hold, around a root job ---------------------------------

//go:embed hold.sh
var holdScript string

// FleetAddrs is where the hook reads the addresses a job may not name.
const FleetAddrs = "/etc/homedash/fleet-addrs"

// ArmJob writes, as root over c, what a job round meets on its own
// tools: the hook, and the hub's and every other remote's address. Both
// are written before every round, so a job that changed them does not
// change the next one's.
func (f *Fleet) ArmJob(ctx context.Context, c *ssh.Client, h *store.Host) error {
	hook := gate.AgentHome + "/.omp/agent/hooks/pre/homedash-refusals.ts"
	if err := remote.WriteFile(ctx, c, hook, []byte(gate.Hook), "0644", true); err != nil {
		return err
	}
	return remote.WriteFile(ctx, c, FleetAddrs, []byte(strings.Join(f.fleetAddrs(ctx, h), "\n")+"\n"), "0644", true)
}

// fleetAddrs is every address the hub knows for itself and for every
// remote but h.
func (f *Fleet) fleetAddrs(ctx context.Context, h *store.Host) []string {
	seen := map[string]bool{}
	var out []string
	add := func(a string) {
		a = strings.TrimSpace(a)
		if a != "" && a != h.Addr && !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	if v, _ := f.Store.Setting(ctx, "hub.lan_addr"); v != "" {
		add(v)
	}
	if f.EnrollURL == nil {
	} else if u, err := url.Parse(f.EnrollURL()); err == nil {
		add(u.Hostname())
	}
	hs, _ := f.Store.Hosts(ctx)
	for _, x := range hs {
		if x.ID != h.ID {
			add(x.Addr)
		}
	}
	return out
}

// holdBody is hold.sh with the hub's key filled in.
func (f *Fleet) holdBody() string {
	return "PUBKEY=" + shq(strings.TrimSpace(f.PubKey)) + "\n" + holdScript
}

// HoldStopPost is the ExecStopPost a job round's unit carries: hold.sh,
// inline so a job cannot edit it, run as root when the unit stops for
// any reason, its result left for CheckHold to report. Base64 keeps it
// clear of systemd's own $ and % expansion.
func (f *Fleet) HoldStopPost() string {
	return `/bin/sh -c "echo ` + base64.StdEncoding.EncodeToString([]byte(f.holdBody())) + ` | base64 -d | sh > /etc/homedash/hold-restored 2>&1"`
}

// CheckHold runs hold.sh as root over c after a job round: whatever of
// the hub's hold the job changed is put back. It returns what was
// restored and anything it could not, both empty when the hold was whole.
func (f *Fleet) CheckHold(ctx context.Context, c *ssh.Client) (restored, broken string, err error) {
	r, err := remote.RunOn(ctx, c, []string{"sudo", "-n", "sh", "-s"}, strings.NewReader(f.holdBody()))
	if err != nil {
		return "", "", err
	}
	if r.ExitCode != 0 {
		return "", "", fmt.Errorf("hold check exited %d: %s", r.ExitCode, tail(r.Stderr))
	}
	var fixed, b []string
	for _, l := range strings.Split(strings.TrimSpace(string(r.Stdout)), "\n") {
		if x, ok := strings.CutPrefix(l, "broken: "); ok {
			b = append(b, x)
		} else {
			fixed = append(fixed, strings.Fields(l)...)
		}
	}
	return strings.Join(fixed, " "), strings.Join(b, "; "), nil
}
