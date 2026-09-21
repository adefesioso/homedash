package fleet

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/adefesioso/homedash/hub/internal/remote"
	"github.com/adefesioso/homedash/hub/internal/store"
)

//go:embed address.sh
var addressScript string

// Address is what the card asks for: an interface held at one address,
// or handed back to DHCP when CIDR is blank.
type Address struct {
	Interface string   `json:"interface"`
	CIDR      string   `json:"address"`
	Gateway   string   `json:"gateway"`
	DNS       []string `json:"dns"`
}

var ifaceName = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,15}$`)

// Validate is the hub's own check before anything reaches the machine:
// a real interface name, an IPv4 host address with its prefix, a
// gateway inside that prefix, DNS servers that are addresses. It never
// touches the remote.
func (a *Address) Validate() error {
	a.Interface = strings.TrimSpace(a.Interface)
	a.CIDR = strings.TrimSpace(a.CIDR)
	a.Gateway = strings.TrimSpace(a.Gateway)
	if !ifaceName.MatchString(a.Interface) {
		return errors.New("interface: a name such as eth0 or enp3s0")
	}
	dns := a.DNS[:0]
	for _, d := range a.DNS {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if ip := net.ParseIP(d); ip == nil || ip.To4() == nil {
			return fmt.Errorf("dns: %q is not an IPv4 address", d)
		}
		dns = append(dns, d)
	}
	a.DNS = dns
	if a.CIDR == "" {
		if a.Gateway != "" || len(a.DNS) > 0 {
			return errors.New("back to DHCP takes no gateway or DNS")
		}
		return nil
	}
	ip, ipnet, err := net.ParseCIDR(a.CIDR)
	if err != nil || ip.To4() == nil {
		return errors.New("address: an IPv4 address with its prefix, such as 192.168.1.20/24")
	}
	ones, _ := ipnet.Mask.Size()
	if ones < 1 || ones > 30 {
		return errors.New("address: a prefix between /1 and /30")
	}
	if ip.Equal(ipnet.IP) || ip.Equal(broadcast(ipnet)) {
		return errors.New("address: the network or broadcast address of its own prefix")
	}
	if a.Gateway != "" {
		gw := net.ParseIP(a.Gateway)
		if gw == nil || gw.To4() == nil {
			return errors.New("gateway: an IPv4 address")
		}
		if !ipnet.Contains(gw) {
			return fmt.Errorf("gateway: %s is not inside %s", a.Gateway, ipnet)
		}
		if gw.Equal(ip) {
			return errors.New("gateway: the same as the address")
		}
	}
	return nil
}

func broadcast(n *net.IPNet) net.IP {
	ip := n.IP.To4()
	b := make(net.IP, 4)
	for i := range b {
		b[i] = ip[i] | ^n.Mask[i]
	}
	return b
}

// confirmScript is the confirmation, run as root at the new address:
// only once the guard has applied the config does the confirm file
// count, and the guard is then given the few seconds it takes to see
// the file and exit, so the change is over — not merely accepted — when
// the hub answers, and a second change can follow at once.
const confirmScript = `test -e /var/lib/homedash/net/applied || exit 1
touch /run/homedash-net-confirm
i=0; while systemctl is-active -q homedash-net-guard 2>/dev/null && [ $i -lt 10 ]; do sleep 1; i=$((i+1)); done
exit 0`

// confirmWindow is how long the hub keeps trying the new address before
// it gives up; the guard on the machine reverts at ninety seconds, so
// the hub stops asking a little before that.
const confirmWindow = 75 * time.Second

// SetAddress holds an interface on a host at a static address, or hands
// it back to DHCP, and never trusts the change on the machine's say-so:
// address.sh writes the config and starts a guard that applies it and
// reverts unless confirmed; the hub then has to reach the machine at the
// new address with the pinned host key, and only that reach confirms
// it. Confirmed, the host's pinned address follows. Not confirmed, the
// machine reverts on its own and the record is unchanged.
func (f *Fleet) SetAddress(ctx context.Context, h *store.Host, a Address) error {
	if err := a.Validate(); err != nil {
		return err
	}
	mode, want := "static", ""
	if a.CIDR == "" {
		mode = "dhcp"
	} else {
		ip, _, _ := net.ParseCIDR(a.CIDR)
		want = ip.String()
	}
	if err := f.launchAddress(ctx, h, mode, a); err != nil {
		return err
	}
	// The machine at the new address, with the enrolled key. Back to
	// DHCP is confirmed at the address the machine had.
	next := *h
	if want != "" {
		next.Addr = want
	}
	if err := f.confirmAddress(ctx, &next); err != nil {
		return err
	}
	if next.Addr != h.Addr {
		if err := f.Store.SetHostAddr(ctx, h.ID, next.Addr); err != nil {
			return err
		}
		f.Drop(h.ID)
		h.Addr = next.Addr
	}
	return nil
}

// launchAddress pipes address.sh as root: the config is written and the
// guard started, and the connection this came in on is still up when
// it answers. Its stdout is the manager it wrote for.
func (f *Fleet) launchAddress(ctx context.Context, h *store.Host, mode string, a Address) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c, err := f.Exec.Dial(ctx, Target(h))
	if err != nil {
		return err
	}
	defer c.Close()
	argv := []string{"sudo", "-n", "sh", "-s", "--", a.Interface, mode, a.CIDR, a.Gateway, strings.Join(a.DNS, " ")}
	r, err := remote.RunOn(ctx, c, argv, strings.NewReader(addressScript))
	if err != nil {
		return err
	}
	if r.ExitCode != 0 {
		return fmt.Errorf("address change refused, machine unchanged: %s", tail(r.Stderr))
	}
	return nil
}

// confirmAddress dials the host as it should now be until the pinned
// key answers there, then touches the guard's confirm file — only once
// the guard has applied the config, so an answer from before the change
// (a hold at the address the machine already had) confirms nothing. A different
// key at that address is refused at once — something else answers
// there — and a window with no answer is the guard's revert.
func (f *Fleet) confirmAddress(ctx context.Context, h *store.Host) error {
	deadline := time.Now().Add(confirmWindow)
	var last error
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
		dctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		c, err := f.Exec.Dial(dctx, Target(h))
		if err == nil {
			r, rerr := remote.RunOn(dctx, c, []string{"sudo", "-n", "sh", "-c", confirmScript}, nil)
			c.Close()
			cancel()
			if rerr == nil && r.ExitCode == 0 {
				return nil
			}
			last = rerr
			continue
		}
		cancel()
		if errors.Is(err, remote.ErrKeyMismatch) {
			return fmt.Errorf("a different host key answered at %s; %s will revert on its own", h.Addr, h.Name)
		}
		last = err
	}
	if last == nil {
		last = errors.New("no answer")
	}
	return fmt.Errorf("the hub could not reach %s at %s (%v); the machine reverts to its previous address on its own", h.Name, h.Addr, last)
}
