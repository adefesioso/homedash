// Package peers is part two: sharing beyond the house. There is no
// server. Hubs find each other over a Kademlia DHT — the public IPFS one
// or a private one of hubs: a shared space name is hashed into a
// rendezvous, and every hub that typed the same name gets a direct,
// encrypted, hole-punched connection. Identity is the connection's
// public key, and only a hub found at the rendezvous is spoken to. Two things cross, both named first —
// inference, and a service published to that peer — and each crosses
// once: a hub serves an arriving job or connection on its own machines
// or refuses it, and the inbound path has no route onward at all.
package peers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/pnet"
	"github.com/libp2p/go-libp2p/core/protocol"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/libp2p/go-libp2p/p2p/host/autorelay"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"

	"github.com/adefesioso/homedash/hub/internal/fleet"
	"github.com/adefesioso/homedash/hub/internal/pool"
	"github.com/adefesioso/homedash/hub/internal/store"
)

const (
	protoOffer = protocol.ID("/homedash/1.0.0/offer")
	protoJob   = protocol.ID("/homedash/1.1.0/job")

	// headerMax bounds every header read from a peer before its key has
	// been checked; bodyMax is the pool's own request ceiling.
	headerMax = 64 << 10
	bodyMax   = 64 << 20
)

// header decodes one JSON value from a stream, reading at most headerMax
// bytes, and returns a reader for what follows it: whatever the decoder
// buffered past the value, then the stream, with the newline the
// encoder wrote after the value skipped.
func header(s io.Reader, v any) (*bufio.Reader, error) {
	dec := json.NewDecoder(io.LimitReader(s, headerMax))
	if err := dec.Decode(v); err != nil {
		return nil, err
	}
	br := bufio.NewReader(io.MultiReader(dec.Buffered(), s))
	if b, _ := br.Peek(1); len(b) == 1 && b[0] == '\n' {
		_, _ = br.ReadByte()
	}
	return br, nil
}

// Offer is what every connection is greeted with, refreshed every
// minute: what this hub can serve, whether it has a free machine, and
// the services published to that peer. A hub advertises only the
// machines it owns, and a service only to the peers it was published to.
type Offer struct {
	Name     string    `json:"name"`
	Models   []string  `json:"models"`
	Free     bool      `json:"free"`
	Services []string  `json:"services,omitempty"`
	At       time.Time `json:"at"`
}

// Peers is the node.
type Peers struct {
	Store    *store.Store
	Pool     *pool.Pool
	Fleet    *fleet.Fleet
	StateDir string
	Notify   fleet.Notify
	Log      *slog.Logger
	// Router is the local router; an inbound job is served through it
	// with the local-only mark, so it cannot go on to another peer.
	Router http.Handler

	mu      sync.Mutex
	host    host.Host
	dht     *dht.IpfsDHT
	cancel  context.CancelFunc
	space   string
	cfg     netConfig
	offers  map[peer.ID]Offer
	serving map[peer.ID]int  // inbound jobs in flight per peer
	total   int              // inbound jobs in flight, all peers
	found   int              // rendezvous peers the last lookup returned
	known   map[peer.ID]bool // hubs seen at the rendezvous; nobody else is spoken to
	redials map[peer.ID]bool // hubs being redialed after their last connection dropped
	conns   atomic.Int64     // service connections being copied, all peers
	perPeer map[peer.ID]int  // service connections being copied, per peer
	buckets map[peer.ID]*bucket
	models  func(context.Context) []string // the pool's, unless a test says otherwise
}

func (p *Peers) poolModels(ctx context.Context) []string {
	if p.models != nil {
		return p.models(ctx)
	}
	return p.Pool.Models(ctx)
}

// netConfig is every setting that shapes the node; a change to any of
// them is a leave and a rejoin.
type netConfig struct {
	Space     string
	Private   bool     // a DHT of hubs, not the IPFS one
	Bootstrap []string // private: multiaddrs of the hubs to join through
	PSK       string   // private: 64 hex, the swarm's pre-shared key
	Reachable bool     // this hub has a public address or a forwarded port
	Port      int      // fixed listen port; 0 is ephemeral
}

// Status is the Peers tab header.
type Status struct {
	Enabled   bool     `json:"enabled"`
	Space     string   `json:"space,omitempty"`
	ID        string   `json:"id,omitempty"`
	Connected int      `json:"connected"`
	Serving   bool     `json:"serving"`
	Addrs     []string `json:"addrs,omitempty"`
	Found     int      `json:"found"`
	Private   bool     `json:"private"`
	Reachable bool     `json:"reachable"`
}

func (p *Peers) settingBool(ctx context.Context, key string) bool {
	v, _ := p.Store.Setting(ctx, key)
	return v == "1" || v == "true" || v == "on"
}

func (p *Peers) settingInt(ctx context.Context, key string, def int) int {
	v, _ := p.Store.Setting(ctx, key)
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
		return n
	}
	return def
}

// config reads the settings that shape the node. An empty Space means
// nothing should run.
func (p *Peers) config(ctx context.Context) netConfig {
	name, _ := p.Store.Setting(ctx, "space.name")
	if !p.settingBool(ctx, "space.enabled") || strings.TrimSpace(name) == "" {
		return netConfig{}
	}
	network, _ := p.Store.Setting(ctx, "space.network")
	boot, _ := p.Store.Setting(ctx, "space.bootstrap")
	psk, _ := p.Store.Setting(ctx, "space.psk")
	return netConfig{
		Space:     strings.TrimSpace(name),
		Private:   strings.TrimSpace(network) == "private",
		Bootstrap: strings.Fields(strings.ReplaceAll(boot, ",", " ")),
		PSK:       strings.ToLower(strings.TrimSpace(psk)),
		Reachable: p.settingBool(ctx, "space.reachable"),
		Port:      p.settingInt(ctx, "space.port", 0),
	}
}

func (c netConfig) equal(o netConfig) bool {
	return c.Space == o.Space && c.Private == o.Private && c.PSK == o.PSK && c.Reachable == o.Reachable &&
		c.Port == o.Port && strings.Join(c.Bootstrap, " ") == strings.Join(o.Bootstrap, " ")
}

// Run watches Settings: a space name that is switched on joins, a change
// to anything that shapes the node leaves and rejoins, off leaves.
// Nothing configured, nothing running.
func (p *Peers) Run(ctx context.Context) {
	for {
		want := p.config(ctx)
		p.mu.Lock()
		cur := p.cfg
		p.mu.Unlock()
		if !cur.equal(want) {
			p.leave()
			if want.Space != "" {
				if err := p.join(ctx, want); err != nil {
					p.Log.Error("join space", "err", err)
				}
			}
		}
		select {
		case <-ctx.Done():
			p.leave()
			return
		case <-time.After(10 * time.Second):
		}
	}
}

// identity is the hub's peer key, generated on first use and kept in the
// state dir. It is the identity every peer verifies by.
func (p *Peers) identity() (crypto.PrivKey, error) {
	path := filepath.Join(p.StateDir, "peer_key")
	if b, err := os.ReadFile(path); err == nil {
		return crypto.UnmarshalPrivateKey(b)
	}
	priv, _, err := crypto.GenerateEd25519Key(nil)
	if err != nil {
		return nil, err
	}
	b, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		return nil, err
	}
	return priv, os.WriteFile(path, b, 0o600)
}

// bootstrapPeers is where the DHT starts: the IPFS nodes on the public
// network, the configured hubs on a private one.
func (c netConfig) bootstrapPeers() []peer.AddrInfo {
	if !c.Private {
		return dht.GetDefaultBootstrapPeerAddrInfos()
	}
	var out []peer.AddrInfo
	for _, a := range c.Bootstrap {
		if ai, err := peer.AddrInfoFromString(a); err == nil {
			out = append(out, *ai)
		}
	}
	return out
}

// limits size the swarm for one small box, before any HomeDash rule
// runs: the DHT alone connects a hub to hundreds of nodes, and a
// stranger's cost has to stop here.
func limits() (network.ResourceManager, *connmgr.BasicConnMgr, error) {
	perPeer := rcmgr.ResourceLimits{StreamsInbound: 8, Streams: 16}
	partial := rcmgr.PartialLimitConfig{
		System:              rcmgr.ResourceLimits{Conns: 512, ConnsInbound: 256, Streams: 2048, StreamsInbound: 1024, FD: 512, Memory: 256 << 20},
		Transient:           rcmgr.ResourceLimits{Conns: 64, ConnsInbound: 32, Streams: 128, StreamsInbound: 64, FD: 64, Memory: 32 << 20},
		PeerDefault:         rcmgr.ResourceLimits{Conns: 8, ConnsInbound: 4, Streams: 64, StreamsInbound: 32, Memory: 16 << 20},
		ProtocolPeerDefault: rcmgr.ResourceLimits{Streams: 64, StreamsInbound: 32},
		ProtocolPeer:        map[protocol.ID]rcmgr.ResourceLimits{protoOffer: perPeer, protoJob: perPeer, protoTCP: {StreamsInbound: 32, Streams: 64}},
	}
	rm, err := rcmgr.NewResourceManager(rcmgr.NewFixedLimiter(partial.Build(rcmgr.DefaultLimits.AutoScale())))
	if err != nil {
		return nil, nil, err
	}
	cm, err := connmgr.NewConnManager(64, 192, connmgr.WithGracePeriod(time.Minute))
	if err != nil {
		return nil, nil, err
	}
	return rm, cm, nil
}

func (p *Peers) join(ctx context.Context, cfg netConfig) error {
	priv, err := p.identity()
	if err != nil {
		return err
	}
	boot := cfg.bootstrapPeers()
	// Relays: any connected peer that runs the relay-v2 hop service — on
	// the public network that is most of it, bootstrap nodes first; on a
	// private one it is the reachable hubs.
	var hostRef host.Host
	var hostMu sync.Mutex
	source := func(ctx context.Context, num int) <-chan peer.AddrInfo {
		out := make(chan peer.AddrInfo, num)
		go func() {
			defer close(out)
			for _, ai := range boot {
				select {
				case out <- ai:
				case <-ctx.Done():
					return
				}
			}
			hostMu.Lock()
			hh := hostRef
			hostMu.Unlock()
			if hh == nil {
				return
			}
			for _, id := range hh.Network().Peers() {
				if ok, _ := hh.Peerstore().SupportsProtocols(id, "/libp2p/circuit/relay/0.2.0/hop"); len(ok) == 0 {
					continue
				}
				select {
				case out <- hh.Peerstore().PeerInfo(id):
				case <-ctx.Done():
					return
				}
			}
		}()
		return out
	}
	rm, cm, err := limits()
	if err != nil {
		return err
	}
	port := strconv.Itoa(cfg.Port)
	listen := []string{"/ip4/0.0.0.0/tcp/" + port, "/ip6/::/tcp/" + port}
	opts := []libp2p.Option{
		libp2p.Identity(priv),
		libp2p.NATPortMap(),
		libp2p.EnableHolePunching(),
		libp2p.ResourceManager(rm),
		libp2p.ConnectionManager(cm),
	}
	if cfg.Private && cfg.PSK != "" {
		// A keyed swarm: the handshake fails without the key. QUIC cannot
		// carry one, so a keyed network listens on TCP only.
		key, err := pnet.DecodeV1PSK(strings.NewReader("/key/swarm/psk/1.0.0/\n/base16/\n" + cfg.PSK + "\n"))
		if err != nil {
			return fmt.Errorf("network key: %w", err)
		}
		opts = append(opts, libp2p.PrivateNetwork(key))
	} else {
		listen = append(listen, "/ip4/0.0.0.0/udp/"+port+"/quic-v1", "/ip6/::/udp/"+port+"/quic-v1")
	}
	opts = append(opts, libp2p.ListenAddrStrings(listen...))
	if cfg.Reachable {
		// A hub with a public address or a forwarded port: say so, and on
		// a private network relay for the members that have neither.
		opts = append(opts, libp2p.ForceReachabilityPublic())
		if cfg.Private {
			opts = append(opts, libp2p.EnableRelayService())
		}
	} else {
		// A home hub is behind NAT; saying so up front lets it reserve a
		// relay slot at once instead of waiting to be told. A relayed
		// connection is what the hole punch upgrades to a direct one.
		opts = append(opts, libp2p.ForceReachabilityPrivate(),
			libp2p.EnableAutoRelayWithPeerSource(source, autorelay.WithNumRelays(2), autorelay.WithMinCandidates(1), autorelay.WithBootDelay(5*time.Second), autorelay.WithMinInterval(30*time.Second)))
	}
	h, err := libp2p.New(opts...)
	if err != nil {
		return err
	}
	hostMu.Lock()
	hostRef = h
	hostMu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	dopts := []dht.Option{dht.Mode(dht.ModeClient), dht.BootstrapPeers(boot...)}
	if cfg.Private {
		// Its own protocol, so it never meets the IPFS DHT; the reachable
		// hubs are the servers.
		dopts = append(dopts, dht.ProtocolPrefix("/homedash"))
		if cfg.Reachable {
			dopts[0] = dht.Mode(dht.ModeServer)
		}
	}
	kad, err := dht.New(h, dopts...)
	if err != nil {
		cancel()
		h.Close()
		return err
	}
	p.attach(ctx, h, kad, cancel, cfg)
	go p.discover(ctx, cfg.Space, boot)
	p.Notify("space.joined", cfg.Space, "joined space "+cfg.Space+" as "+h.ID().String())
	return nil
}

// attach makes a host this node: state, handlers and the notifier that
// greets a known hub as soon as it connects.
func (p *Peers) attach(ctx context.Context, h host.Host, kad *dht.IpfsDHT, cancel context.CancelFunc, cfg netConfig) {
	p.mu.Lock()
	p.host, p.dht, p.cancel, p.space, p.cfg = h, kad, cancel, cfg.Space, cfg
	p.offers = map[peer.ID]Offer{}
	p.serving = map[peer.ID]int{}
	p.known = map[peer.ID]bool{}
	p.redials = map[peer.ID]bool{}
	p.perPeer = map[peer.ID]int{}
	p.buckets = map[peer.ID]*bucket{}
	p.mu.Unlock()
	h.SetStreamHandler(protoOffer, p.handleOffer)
	h.SetStreamHandler(protoJob, p.handleJob)
	h.SetStreamHandler(protoTCP, p.handleTCP)
	h.Network().Notify(&network.NotifyBundle{
		// Only hubs matter: the DHT connects us to hundreds of nodes that
		// are not HomeDash and would not understand an offer.
		ConnectedF: func(_ network.Network, c network.Conn) {
			p.mu.Lock()
			hub := p.known[c.RemotePeer()]
			p.mu.Unlock()
			if hub {
				go p.greet(ctx, c.RemotePeer())
			}
		},
		DisconnectedF: func(n network.Network, c network.Conn) {
			id := c.RemotePeer()
			if cs := n.Connectedness(id); cs == network.Connected || cs == network.Limited {
				return
			}
			p.mu.Lock()
			delete(p.offers, id)
			hub := p.known[id]
			p.mu.Unlock()
			if hub {
				go p.redial(ctx, id)
			}
		},
	})
}

// redial brings a hub back as soon as its last connection drops. A
// public relay resets every relayed connection after two minutes or
// 128 KB, and waiting for the next discovery tick would leave the peer
// gone for most of that. Backs off from a second to half a minute until
// the hub is connected again or the node leaves; one redial per hub.
func (p *Peers) redial(ctx context.Context, id peer.ID) {
	p.mu.Lock()
	h, kad := p.host, p.dht
	if h == nil || p.redials == nil || p.redials[id] {
		p.mu.Unlock()
		return
	}
	p.redials[id] = true
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		if p.redials != nil {
			delete(p.redials, id)
		}
		p.mu.Unlock()
	}()
	wait := time.Second
	for ctx.Err() == nil {
		if cs := h.Network().Connectedness(id); cs == network.Connected || cs == network.Limited {
			return
		}
		c, cancel := context.WithTimeout(ctx, 30*time.Second)
		ai := peer.AddrInfo{ID: id}
		if len(h.Peerstore().Addrs(id)) == 0 {
			// The relay addresses expired with the connection; ask the DHT.
			if full, err := kad.FindPeer(c, id); err == nil {
				ai = full
			}
		}
		err := h.Connect(c, ai)
		cancel()
		if err == nil {
			return
		}
		p.Log.Info("redial peer", "peer", short(id), "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if wait *= 2; wait > 30*time.Second {
			wait = 30 * time.Second
		}
	}
}

func (p *Peers) leave() {
	p.mu.Lock()
	h, kad, cancel, space := p.host, p.dht, p.cancel, p.space
	p.host, p.dht, p.cancel, p.space, p.cfg = nil, nil, nil, "", netConfig{}
	p.offers, p.serving, p.known, p.redials, p.perPeer, p.buckets = nil, nil, nil, nil, nil, nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if kad != nil {
		_ = kad.Close()
	}
	if h != nil {
		_ = h.Close()
		p.Notify("space.left", space, "left space "+space)
	}
}

// rendezvous is the space name hashed: the name is the rendezvous.
func rendezvous(space string) string {
	sum := sha256.Sum256([]byte("homedash/" + space))
	return "homedash/" + hex.EncodeToString(sum[:])
}

// discover bootstraps the DHT, advertises the rendezvous, and keeps
// finding and dialing the hubs that advertised the same one.
func (p *Peers) discover(ctx context.Context, space string, boot []peer.AddrInfo) {
	p.mu.Lock()
	h, kad := p.host, p.dht
	p.mu.Unlock()
	if h == nil {
		return
	}
	if err := kad.Bootstrap(ctx); err != nil {
		p.Log.Warn("dht bootstrap", "err", err)
	}
	// Connect to the bootstrap peers ourselves; the DHT only knows them.
	for _, ai := range boot {
		go func(ai peer.AddrInfo) {
			c, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			_ = h.Connect(c, ai)
		}(ai)
	}
	disc := drouting.NewRoutingDiscovery(kad)
	ns := rendezvous(space)
	dutil.Advertise(ctx, disc, ns)
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		lookup, lcancel := context.WithTimeout(ctx, 45*time.Second)
		peersCh, err := disc.FindPeers(lookup, ns)
		found := 0
		if err == nil {
			for ai := range peersCh {
				if ai.ID == h.ID() {
					continue
				}
				found++
				p.mu.Lock()
				if p.known != nil {
					p.known[ai.ID] = true
				}
				p.mu.Unlock()
				// A hub is never what the connection manager trims.
				h.ConnManager().Protect(ai.ID, "homedash")
				if c := h.Network().Connectedness(ai.ID); c == network.Connected || c == network.Limited {
					go p.greet(ctx, ai.ID)
					continue
				}
				go func(ai peer.AddrInfo) {
					c, cancel := context.WithTimeout(ctx, 60*time.Second)
					defer cancel()
					if len(ai.Addrs) == 0 {
						// The record carried no address; ask the DHT for one.
						if full, err := kad.FindPeer(c, ai.ID); err == nil {
							ai = full
						}
					}
					if err := h.Connect(c, ai); err != nil {
						p.Log.Info("dial rendezvous peer", "peer", ai.ID, "addrs", len(ai.Addrs), "err", err)
					}
				}(ai)
			}
		} else {
			p.Log.Warn("rendezvous lookup", "err", err)
		}
		lcancel()
		p.mu.Lock()
		p.found = found
		p.mu.Unlock()
		// Refresh the offers with every hub we are connected to, and keep
		// asking for a direct path while a relay is all there is.
		for _, id := range h.Network().Peers() {
			p.mu.Lock()
			hub := p.known[id]
			p.mu.Unlock()
			if hub {
				go p.greet(ctx, id)
				if p.limited(id) {
					go func(id peer.ID) {
						c, cancel := context.WithTimeout(ctx, 40*time.Second)
						defer cancel()
						_ = h.Connect(network.WithForceDirectDial(c, "homedash direct"), peer.AddrInfo{ID: id})
					}(id)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// offer is what this hub says about itself right now, to one peer.
func (p *Peers) offer(ctx context.Context, to peer.ID) Offer {
	name, _ := p.Store.Setting(ctx, "space.hub_name")
	if name == "" {
		name, _ = os.Hostname()
	}
	models := p.Pool.Models(ctx)
	free := false
	if grid, err := p.Pool.Grid(ctx); err == nil {
		for _, m := range grid {
			if m.Online && m.Enabled && !m.Busy && len(m.Models) > 0 {
				free = true
			}
		}
	}
	if !p.settingBool(ctx, "space.serve") {
		// A hub that does not run peers' jobs offers nothing.
		models, free = nil, false
	}
	return Offer{Name: name, Models: models, Free: free, Services: p.published(ctx, to), At: time.Now()}
}

// greet sends our offer to a peer and reads theirs back.
func (p *Peers) greet(ctx context.Context, id peer.ID) {
	p.mu.Lock()
	h := p.host
	p.mu.Unlock()
	if h == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	// An offer is small: it may travel over a relayed (limited) connection,
	// which is what two hubs behind NATs have until the hole punch lands.
	s, err := h.NewStream(network.WithAllowLimitedConn(ctx, "homedash offer"), id, protoOffer)
	if err != nil {
		p.Log.Info("greet peer", "peer", short(id), "err", err)
		return
	}
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(20 * time.Second))
	if err := json.NewEncoder(s).Encode(p.offer(ctx, id)); err != nil {
		return
	}
	var theirs Offer
	if _, err := header(s, &theirs); err != nil {
		return
	}
	p.learn(ctx, id, theirs)
}

// isKnown reports whether a peer was found at the rendezvous. Nobody
// else is spoken to: the DHT connects a hub to hundreds of nodes that
// are not HomeDash, and identify tells each of them what this host
// speaks.
func (p *Peers) isKnown(id peer.ID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.known[id]
}

// handleOffer answers a greeting with our own offer — from a hub found
// at the rendezvous. Anyone else is reset unread.
func (p *Peers) handleOffer(s network.Stream) {
	from := s.Conn().RemotePeer()
	if !p.isKnown(from) {
		s.Reset()
		return
	}
	defer s.Close()
	ctx := context.Background()
	_ = s.SetDeadline(time.Now().Add(20 * time.Second))
	var theirs Offer
	if _, err := header(s, &theirs); err != nil {
		return
	}
	p.learn(ctx, from, theirs)
	_ = json.NewEncoder(s).Encode(p.offer(ctx, from))
}

func (p *Peers) learn(ctx context.Context, id peer.ID, o Offer) {
	o.At = time.Now()
	p.mu.Lock()
	if p.offers != nil {
		p.offers[id] = o
	}
	p.mu.Unlock()
	unknown, _ := p.Store.Setting(ctx, "space.unknown")
	_ = p.Store.SeenPeer(ctx, id.String(), o.Name, unknown == "accept",
		p.settingInt(ctx, "space.default_concurrent", 1), p.settingInt(ctx, "space.default_per_hour", 20))
	// A service this peer published to us appears on its row, unapproved,
	// the first time its offer names it; approval is a person's act.
	for _, svc := range o.Services {
		_ = p.Store.SeenFront(ctx, id.String(), svc)
	}
}

// --- sending a job ----------------------------------------------------

// jobHeader is what a peer reads before deciding: the model, the path
// and how long the body is. The body follows only after a yes.
type jobHeader struct {
	Model string `json:"model"`
	Path  string `json:"path"`
	Size  int64  `json:"size"`
}

type jobReply struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// Place is the pool's seam: try the approved peers whose latest offer
// fits, best record first. A peer with no record is tried rather than
// skipped; a peer nobody here approved is never sent a prompt, whatever
// it offers.
func (p *Peers) Place(ctx context.Context, model, path string, body []byte) (io.ReadCloser, string, error) {
	p.mu.Lock()
	h := p.host
	type cand struct {
		id peer.ID
		o  Offer
	}
	var cands []cand
	for id, o := range p.offers {
		if time.Since(o.At) < 3*time.Minute && o.Free && contains(o.Models, model) {
			cands = append(cands, cand{id, o})
		}
	}
	p.mu.Unlock()
	if h == nil || len(cands) == 0 {
		return nil, "", pool.ErrNoPeer
	}
	approved := cands[:0]
	for _, c := range cands {
		if pr, err := p.Store.Peer(ctx, c.id.String()); err == nil && pr.Approved {
			approved = append(approved, c)
		}
	}
	cands = approved
	if len(cands) == 0 {
		return nil, "", pool.ErrNoPeer
	}
	// The record decides the order: a claim from a hub that answered every
	// time is worth more than the same claim from one that refused.
	scores := map[peer.ID]float64{}
	for _, c := range cands {
		r, _ := p.Store.PeerRecord(ctx, c.id.String())
		s := 1.0
		if r.Samples > 0 || r.Accepted > 0 {
			s = 0.5*r.Accepted + 0.3*r.Finished + 0.2/(1+r.MedianTTFT/2000)
		}
		scores[c.id] = s
	}
	sortCands(cands, func(a, b cand) bool { return scores[a.id] > scores[b.id] })
	// A candidate tried and refused is worth reporting — the caller's
	// decision names it and says why — so the last one's name and reason
	// survive the loop rather than collapsing into ErrNoPeer, which is
	// reserved for nobody having been asked at all.
	var lastName string
	var lastErr error
	for _, c := range cands {
		rc, err := p.send(ctx, c.id, c.o.Name, model, path, body)
		if err == nil {
			return rc, c.o.Name, nil
		}
		p.Log.Info("peer refused", "peer", c.o.Name, "err", err)
		lastName, lastErr = c.o.Name, err
	}
	return nil, lastName, lastErr
}

func (p *Peers) send(ctx context.Context, id peer.ID, name, model, path string, body []byte) (io.ReadCloser, error) {
	p.mu.Lock()
	h := p.host
	p.mu.Unlock()
	// A relayed connection is what two hubs behind NATs start with. Ask
	// for a direct one — this is what starts the hole punch — and give it
	// a moment; if it does not land, the relayed path carries the job
	// with the relay's limits, which a short answer fits in.
	if p.limited(id) {
		dctx, cancel := context.WithTimeout(ctx, 25*time.Second)
		_ = h.Connect(network.WithForceDirectDial(dctx, "homedash job"), peer.AddrInfo{ID: id})
		cancel()
	}
	sctx := ctx
	if p.limited(id) {
		sctx = network.WithAllowLimitedConn(ctx, "homedash job over relay")
		p.Log.Info("job to peer over a relayed connection; the hole punch has not landed yet", "peer", name)
	}
	s, err := h.NewStream(sctx, id, protoJob)
	if err != nil {
		return nil, err
	}
	if err := json.NewEncoder(s).Encode(jobHeader{Model: model, Path: path, Size: int64(len(body))}); err != nil {
		s.Reset()
		return nil, err
	}
	// The response follows the reply on the same stream.
	var reply jobReply
	_ = s.SetReadDeadline(time.Now().Add(30 * time.Second))
	br, err := header(s, &reply)
	if err != nil {
		s.Reset()
		return nil, err
	}
	_ = s.SetReadDeadline(time.Time{})
	rowID, _ := p.Store.RecordPeerJob(ctx, id.String(), "sent", model, reply.OK, reply.Reason)
	if !reply.OK {
		s.Close()
		return nil, errors.New(reply.Reason)
	}
	// The yes came from a hub that has checked our key; now the prompt.
	if _, err := s.Write(body); err != nil {
		s.Reset()
		return nil, err
	}
	started := time.Now()
	return &recording{r: br, s: s, onFirst: func() { _ = p.Store.FinishPeerJob(ctx, rowID, false, time.Since(started)) },
		onClose: func(ok bool) { _ = p.Store.FinishPeerJob(context.Background(), rowID, ok, 0) }}, nil
}

// recording wraps the stream to record the first token and the end.
type recording struct {
	r       io.Reader
	s       network.Stream
	first   bool
	onFirst func()
	onClose func(bool)
	eof     bool
}

func (r *recording) Read(b []byte) (int, error) {
	n, err := r.r.Read(b)
	if n > 0 && !r.first {
		r.first = true
		r.onFirst()
	}
	if errors.Is(err, io.EOF) {
		r.eof = true
	}
	return n, err
}

func (r *recording) Close() error {
	r.onClose(r.eof)
	return r.s.Close()
}

// --- serving a job ----------------------------------------------------

// handleJob is B's side: verify the sender by the connection's key,
// decide (approved, under its concurrency, under its hour), route the
// job locally through exactly the placement path a request of our own
// takes — marked local-only, so it can never go on to a third hub — and
// stream the result back.
func (p *Peers) handleJob(s network.Stream) {
	defer s.Close()
	ctx := context.Background()
	from := s.Conn().RemotePeer()
	if !p.isKnown(from) {
		s.Reset()
		return
	}
	// The header is a few bytes and is all that is read before the
	// sender's key has been checked; the JSON decoder buffers, so the
	// body is read from the stream itself once there is a yes.
	var hdr jobHeader
	_ = s.SetReadDeadline(time.Now().Add(30 * time.Second))
	br, err := header(s, &hdr)
	if err != nil {
		s.Reset()
		return
	}
	_ = s.SetReadDeadline(time.Time{})
	refuse := func(reason string) {
		_, _ = p.Store.RecordPeerJob(ctx, from.String(), "served", hdr.Model, false, reason)
		_ = json.NewEncoder(s).Encode(jobReply{Reason: reason})
		p.Notify("peer.refused", from.String(), fmt.Sprintf("refused a job for %s from %s: %s", hdr.Model, short(from), reason))
	}
	if !p.settingBool(ctx, "space.serve") {
		refuse("this hub does not run peers' jobs")
		return
	}
	pr, err := p.Store.Peer(ctx, from.String())
	if err != nil || !pr.Approved {
		refuse("not approved")
		return
	}
	if !pool.JobPaths[hdr.Path] {
		refuse("not a prompt: " + hdr.Path)
		return
	}
	if hdr.Size < 0 || hdr.Size > bodyMax {
		refuse("request too large")
		return
	}
	n, _ := p.Store.ServedInLastHour(ctx, from.String())
	if n >= pr.PerHour {
		refuse(fmt.Sprintf("over the hourly allowance (%d)", pr.PerHour))
		return
	}
	if all, _ := p.Store.ServedInLastHourAll(ctx); all >= p.settingInt(ctx, "space.per_hour", 100) {
		refuse("over the hub's hourly allowance")
		return
	}
	ceiling := p.settingInt(ctx, "space.ceiling", 2)
	p.mu.Lock()
	if p.serving[from] >= pr.MaxConcurrent || p.total >= ceiling {
		p.mu.Unlock()
		refuse("over the concurrency limit")
		return
	}
	p.serving[from]++
	p.total++
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.serving[from]--
		p.total--
		p.mu.Unlock()
	}()
	if !contains(p.poolModels(ctx), hdr.Model) {
		refuse("nothing in the pool holds " + hdr.Model)
		return
	}
	rowID, _ := p.Store.RecordPeerJob(ctx, from.String(), "served", hdr.Model, true, "")
	if err := json.NewEncoder(s).Encode(jobReply{OK: true}); err != nil {
		return
	}
	body := make([]byte, hdr.Size)
	_ = s.SetReadDeadline(time.Now().Add(60 * time.Second))
	if _, err := io.ReadFull(br, body); err != nil {
		s.Reset()
		_ = p.Store.FinishPeerJob(ctx, rowID, false, 0)
		return
	}
	_ = s.SetReadDeadline(time.Time{})
	req := httptest.NewRequest(http.MethodPost, hdr.Path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-HomeDash-Local-Only", "1")
	w := &streamWriter{s: s, header: http.Header{}}
	p.Router.ServeHTTP(w, req)
	_ = p.Store.FinishPeerJob(ctx, rowID, w.status < 400, 0)
}

// streamWriter is an http.ResponseWriter onto the stream: a peer gets
// the body bytes and nothing else.
type streamWriter struct {
	s      network.Stream
	header http.Header
	status int
}

func (w *streamWriter) Header() http.Header { return w.header }
func (w *streamWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
}
func (w *streamWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.s.Write(b)
}
func (w *streamWriter) Flush() {}

// --- the tab ----------------------------------------------------------

// Row is one peer as the tab shows it.
type Row struct {
	store.Peer
	Connected bool         `json:"connected"`
	Direct    bool         `json:"direct"` // a hole-punched or public path, not a relay
	Offer     *Offer       `json:"offer,omitempty"`
	Record    store.Record `json:"record"`
	Counts    store.Counts `json:"counts"`
	Fronts    []FrontRow   `json:"fronts"`
}

// FrontRow is one of a peer's services as this hub may front it, and
// whether the peer's latest offer still names it.
type FrontRow struct {
	store.Front
	Offered bool `json:"offered"`
}

// Rows lists every peer discovered with its controls, offer, record and
// counts.
func (p *Peers) Rows(ctx context.Context) ([]Row, error) {
	ps, err := p.Store.Peers(ctx)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	h := p.host
	offers := map[peer.ID]Offer{}
	for k, v := range p.offers {
		offers[k] = v
	}
	p.mu.Unlock()
	out := make([]Row, 0, len(ps))
	for _, pr := range ps {
		r := Row{Peer: pr}
		if id, err := peer.Decode(pr.ID); err == nil {
			if h != nil {
				switch h.Network().Connectedness(id) {
				case network.Connected:
					r.Connected, r.Direct = true, true
				case network.Limited:
					r.Connected = true
				}
				for _, c := range h.Network().ConnsToPeer(id) {
					if c.Stat().Limited {
						r.Direct = false
					}
				}
			}
			if o, ok := offers[id]; ok {
				oc := o
				r.Offer = &oc
			}
		}
		r.Record, _ = p.Store.PeerRecord(ctx, pr.ID)
		r.Counts, _ = p.Store.PeerCounts(ctx, pr.ID)
		fronts, _ := p.Store.Fronts(ctx, pr.ID)
		r.Fronts = []FrontRow{}
		for _, f := range fronts {
			r.Fronts = append(r.Fronts, FrontRow{Front: f, Offered: r.Offer != nil && contains(r.Offer.Services, f.Service)})
		}
		out = append(out, r)
	}
	return out, nil
}

// Status is the tab header.
func (p *Peers) GetStatus(ctx context.Context) Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := Status{Enabled: p.host != nil, Space: p.space, Serving: p.settingBool(ctx, "space.serve"), Private: p.cfg.Private, Reachable: p.cfg.Reachable}
	if p.host != nil {
		st.ID = p.host.ID().String()
		st.Connected = len(p.host.Network().Peers())
		for _, a := range p.host.Addrs() {
			st.Addrs = append(st.Addrs, a.String())
		}
		st.Found = p.found
	}
	return st
}

// limited reports whether every connection to a peer goes through a
// relay.
func (p *Peers) limited(id peer.ID) bool {
	p.mu.Lock()
	h := p.host
	p.mu.Unlock()
	if h == nil {
		return false
	}
	conns := h.Network().ConnsToPeer(id)
	if len(conns) == 0 {
		return false
	}
	for _, c := range conns {
		if !c.Stat().Limited {
			return false
		}
	}
	return true
}

// Offered is the model names every currently-connected peer is
// advertising right now, deduped and sorted — pool.Pool's seam for
// listing what a picker on this hub can additionally reach through the
// space, on top of what this pool holds itself. An offer past its
// 3-minute freshness (Place's own bar for a candidate) doesn't count,
// and neither does a peer with nothing to serve or no free machine.
func (p *Peers) Offered(ctx context.Context) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, o := range p.offers {
		if time.Since(o.At) > 3*time.Minute || !o.Free {
			continue
		}
		for _, m := range o.Models {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	sort.Strings(out)
	return out
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x || y == x+":latest" {
			return true
		}
	}
	return false
}

func short(id peer.ID) string {
	s := id.String()
	if len(s) > 12 {
		return "…" + s[len(s)-8:]
	}
	return s
}

func sortCands[T any](xs []T, less func(a, b T) bool) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && less(xs[j], xs[j-1]); j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}
