package peers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"golang.org/x/time/rate"
)

// protoTCP carries one TCP connection for a published service: a
// service name, a yes or a reason, then bytes both ways until either
// side closes. One stream per connection, so WebSockets and long polls
// need nothing special.
const protoTCP = protocol.ID("/homedash/1.0.0/tcp")

type tcpHeader struct {
	Service string `json:"service"`
}

// published lists the names of the services this hub offers to one peer:
// only the ones published to that peer id, never the rest.
func (p *Peers) published(ctx context.Context, id peer.ID) []string {
	svcs, err := p.Store.Services(ctx)
	if err != nil {
		return nil
	}
	var out []string
	for _, s := range svcs {
		for _, pid := range s.Peers {
			if pid == id.String() {
				out = append(out, s.Name)
				break
			}
		}
	}
	return out
}

// handleTCP is the origin side: verify the sender by the connection's
// key, decide, then copy one connection to the service's port through
// the SSH connection the hub holds to that host. A connection is served
// on this hub's own machine or refused, never passed on.
func (p *Peers) handleTCP(s network.Stream) {
	ctx := context.Background()
	from := s.Conn().RemotePeer()
	if !p.isKnown(from) {
		s.Reset()
		return
	}
	var hdr tcpHeader
	_ = s.SetReadDeadline(time.Now().Add(30 * time.Second))
	br, err := header(s, &hdr)
	if err != nil {
		s.Reset()
		return
	}
	_ = s.SetReadDeadline(time.Time{})
	refuse := func(reason string) {
		_ = json.NewEncoder(s).Encode(jobReply{Reason: reason})
		s.Close()
		p.Notify("service.refused", hdr.Service, fmt.Sprintf("refused a connection to %s from %s: %s", hdr.Service, short(from), reason))
	}
	pr, err := p.Store.Peer(ctx, from.String())
	if err != nil || !pr.Approved {
		refuse("not approved")
		return
	}
	svc, err := p.Store.Service(ctx, hdr.Service)
	if err != nil || !contains(svc.Peers, from.String()) {
		refuse("not published to you")
		return
	}
	h, err := p.Store.Host(ctx, strconv.FormatInt(svc.HostID, 10))
	if err != nil || h.Status != "online" {
		refuse(svc.Host + " is not online")
		return
	}
	ceiling := int64(p.settingInt(ctx, "space.connections", 64))
	if p.conns.Add(1) > ceiling {
		p.conns.Add(-1)
		refuse(fmt.Sprintf("over the connection ceiling (%d)", ceiling))
		return
	}
	defer p.conns.Add(-1)
	// One peer's share of the ceiling, so a single front cannot hold all
	// of it.
	share := p.settingInt(ctx, "space.peer_connections", 16)
	p.mu.Lock()
	over := p.perPeer == nil || p.perPeer[from] >= share
	if !over {
		p.perPeer[from]++
	}
	p.mu.Unlock()
	if over {
		refuse(fmt.Sprintf("over your connection share (%d)", share))
		return
	}
	defer func() {
		p.mu.Lock()
		if p.perPeer != nil {
			p.perPeer[from]--
		}
		p.mu.Unlock()
	}()
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	rc, err := p.Fleet.Port(dctx, h, svc.Port)
	cancel()
	if err != nil {
		refuse(fmt.Sprintf("%s:%d did not answer", svc.Host, svc.Port))
		return
	}
	defer rc.Close()
	defer s.Close()
	if err := json.NewEncoder(s).Encode(jobReply{OK: true}); err != nil {
		return
	}
	n := copyBoth(rc, br, s, p.throttle(ctx, from))
	_ = p.Store.AddPeerBytes(ctx, from.String(), svc.Name, "origin", n)
}

// bucket is one peer's bandwidth allowance for fronting, both
// directions together.
type bucket struct {
	lim *rate.Limiter
}

// throttle is the token bucket for one peer, sized by space.peer_kbps;
// nil when the setting is blank, which is unlimited.
func (p *Peers) throttle(ctx context.Context, id peer.ID) *bucket {
	kbps := p.settingInt(ctx, "space.peer_kbps", 0)
	if kbps <= 0 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.buckets == nil {
		return nil
	}
	b, ok := p.buckets[id]
	if !ok || b.lim.Limit() != rate.Limit(kbps*1024) {
		b = &bucket{lim: rate.NewLimiter(rate.Limit(kbps*1024), kbps*1024)}
		p.buckets[id] = b
	}
	return b
}

// wait spends n bytes of the allowance, in burst-sized pieces.
func (b *bucket) wait(n int) {
	if b == nil {
		return
	}
	for n > 0 {
		k := n
		if k > b.lim.Burst() {
			k = b.lim.Burst()
		}
		_ = b.lim.WaitN(context.Background(), k)
		n -= k
	}
}

// throttled is a writer that spends the bucket before each write.
type throttled struct {
	w io.Writer
	b *bucket
}

func (t throttled) Write(p []byte) (int, error) {
	t.b.wait(len(p))
	return t.w.Write(p)
}

// DialService is the front side: one connection to a peer's service,
// as a net.Conn the server's proxies dial instead of a socket. The
// bytes are counted as fronted for that peer when it closes.
func (p *Peers) DialService(ctx context.Context, peerID, service string) (net.Conn, error) {
	id, err := peer.Decode(peerID)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	h := p.host
	p.mu.Unlock()
	if h == nil {
		return nil, errors.New("not in a space")
	}
	sctx := ctx
	if p.limited(id) {
		sctx = network.WithAllowLimitedConn(ctx, "homedash service over relay")
	}
	s, err := h.NewStream(sctx, id, protoTCP)
	if err != nil {
		return nil, err
	}
	if err := json.NewEncoder(s).Encode(tcpHeader{Service: service}); err != nil {
		s.Reset()
		return nil, err
	}
	var reply jobReply
	_ = s.SetReadDeadline(time.Now().Add(30 * time.Second))
	br, err := header(s, &reply)
	if err != nil {
		s.Reset()
		return nil, err
	}
	_ = s.SetReadDeadline(time.Time{})
	if !reply.OK {
		s.Close()
		return nil, errors.New(reply.Reason)
	}
	c := &streamConn{Stream: s, r: br}
	c.onClose = func(n int64) { _ = p.Store.AddPeerBytes(context.Background(), peerID, service, "fronted", n) }
	return c, nil
}

// streamConn is a libp2p stream as a net.Conn, reading through the
// buffered reader that consumed the reply.
type streamConn struct {
	network.Stream
	r       io.Reader
	n       atomic.Int64
	onClose func(int64)
	closed  atomic.Bool
}

func (c *streamConn) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	c.n.Add(int64(n))
	return n, err
}

func (c *streamConn) Write(b []byte) (int, error) {
	n, err := c.Stream.Write(b)
	c.n.Add(int64(n))
	return n, err
}

func (c *streamConn) Close() error {
	if c.closed.CompareAndSwap(false, true) && c.onClose != nil {
		c.onClose(c.n.Load())
	}
	return c.Stream.Close()
}

func (c *streamConn) LocalAddr() net.Addr  { return addr(c.Stream.Conn().LocalMultiaddr().String()) }
func (c *streamConn) RemoteAddr() net.Addr { return addr(c.Stream.Conn().RemoteMultiaddr().String()) }

type addr string

func (a addr) Network() string { return "libp2p" }
func (a addr) String() string  { return string(a) }

// copyBoth copies a connection both ways until either side ends and
// returns the bytes carried in both directions together. A non-nil
// bucket paces both directions.
func copyBoth(rc net.Conn, from io.Reader, to io.Writer, b *bucket) int64 {
	var total atomic.Int64
	done := make(chan struct{}, 2)
	var rw io.Writer = rc
	if b != nil {
		rw, to = throttled{rc, b}, throttled{to, b}
	}
	go func() {
		n, _ := io.Copy(rw, from)
		total.Add(n)
		if cw, ok := rc.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		done <- struct{}{}
	}()
	go func() {
		n, _ := io.Copy(to, rc)
		total.Add(n)
		if cw, ok := to.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		done <- struct{}{}
	}()
	<-done
	<-done
	return total.Load()
}
