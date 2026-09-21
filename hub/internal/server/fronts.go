package server

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

// A peer's service, fronted here. Two ways: as a link under the panel's
// sign-in, or as an ingress on a public hostname with its own
// certificate and no sign-in of its own. Both proxy to a /tcp stream
// dialed to the peer, so a front never touches a socket of its own.

// frontProxy is one reverse proxy onto one peer's service.
func (s *Server) frontProxy(peerID, service, prefix string) http.Handler {
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(&url.URL{Scheme: "http", Host: service})
			pr.Out.Host = pr.In.Host
			pr.SetXForwarded()
			if prefix != "" {
				pr.Out.Header.Set("X-Forwarded-Prefix", prefix)
			}
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return s.Peers.DialService(ctx, peerID, service)
			},
			ResponseHeaderTimeout: 2 * time.Minute,
			DisableCompression:    true,
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, service+": "+err.Error(), http.StatusBadGateway)
		},
	}
	return rp
}

// linkFront serves /~{peer}/{service}/… to a signed-in user of either
// role: the service appears in this panel under its own address, behind
// this panel's sign-in.
func (s *Server) linkFront(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/~")
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		http.NotFound(w, r)
		return
	}
	peerID, service := parts[0], parts[1]
	if len(parts) == 2 {
		// A service lives under a directory, so its relative links resolve.
		http.Redirect(w, r, r.URL.Path+"/", http.StatusFound)
		return
	}
	f, err := s.Store.Front(r.Context(), peerID, service)
	if err != nil || !f.Approved {
		http.Error(w, "this service is not fronted here", http.StatusNotFound)
		return
	}
	prefix := "/~" + peerID + "/" + service
	http.StripPrefix(prefix, s.frontProxy(peerID, service, prefix)).ServeHTTP(w, r)
}

// ingress keeps the :443 and :80 listeners up while any approved front
// carries a hostname, and down otherwise. Certificates are autocert's
// business; the cache is acme/ under the state dir.
type ingress struct {
	s        *Server
	stateDir string
	mu       sync.Mutex
	https    *http.Server
	http     *http.Server
	failed   bool
}

// Run watches the fronts table and starts or stops the listeners as
// hostnames appear and go.
func (s *Server) Ingress(ctx context.Context, stateDir string) {
	in := &ingress{s: s, stateDir: stateDir}
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		in.reconcile(ctx)
		select {
		case <-ctx.Done():
			in.stop()
			return
		case <-tick.C:
		}
	}
}

func (in *ingress) hostnames(ctx context.Context) []string {
	fronts, err := in.s.Store.Fronts(ctx, "")
	if err != nil {
		return nil
	}
	var out []string
	for _, f := range fronts {
		if f.Approved && f.Hostname != "" {
			out = append(out, strings.ToLower(f.Hostname))
		}
	}
	return out
}

func (in *ingress) reconcile(ctx context.Context) {
	names := in.hostnames(ctx)
	in.mu.Lock()
	defer in.mu.Unlock()
	if len(names) == 0 {
		in.stopLocked()
		return
	}
	if in.https != nil {
		return
	}
	m := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(filepath.Join(in.stateDir, "acme")),
		HostPolicy: func(ctx context.Context, host string) error { return in.policy(ctx, host) },
	}
	// Host-routed: the hostname names the front, the front names the peer
	// and the service. No sign-in of the hub's own: the app's is what a
	// visitor meets.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.Host)
		if host == "" {
			host = r.Host
		}
		f, err := in.s.Store.FrontByHostname(r.Context(), strings.ToLower(host))
		if err != nil {
			http.Error(w, "no service at this name", http.StatusNotFound)
			return
		}
		in.s.frontProxy(f.PeerID, f.Service, "").ServeHTTP(w, r)
	})
	httpsLn, err := net.Listen("tcp", ":443")
	if err != nil {
		in.fail(err)
		return
	}
	httpLn, err := net.Listen("tcp", ":80")
	if err != nil {
		httpsLn.Close()
		in.fail(err)
		return
	}
	in.https = &http.Server{Handler: handler, TLSConfig: &tls.Config{GetCertificate: m.GetCertificate, NextProtos: []string{"h2", "http/1.1", "acme-tls/1"}}, ReadHeaderTimeout: 10 * time.Second}
	in.http = &http.Server{Handler: m.HTTPHandler(nil), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = in.https.ServeTLS(httpsLn, "", "") }()
	go func() { _ = in.http.Serve(httpLn) }()
	in.failed = false
	in.s.log.Info("ingress listening", "hostnames", names)
}

// policy is autocert's host policy: only a hostname an approved front
// carries gets a certificate.
func (in *ingress) policy(ctx context.Context, host string) error {
	for _, n := range in.hostnames(ctx) {
		if n == strings.ToLower(host) {
			return nil
		}
	}
	return errors.New("no front carries " + host)
}

func (in *ingress) fail(err error) {
	if !in.failed {
		in.failed = true
		in.s.Notify("ingress.failed", "ingress", "the ingress could not listen on :443/:80: "+err.Error()+" (retrying each minute)")
	}
}

func (in *ingress) stop() {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.stopLocked()
}

func (in *ingress) stopLocked() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if in.https != nil {
		_ = in.https.Shutdown(ctx)
		in.https = nil
	}
	if in.http != nil {
		_ = in.http.Shutdown(ctx)
		in.http = nil
	}
}
