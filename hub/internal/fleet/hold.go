package fleet

import (
	"context"
	"net"
	"strconv"
	"sync"

	"golang.org/x/crypto/ssh"

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
