package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// login is the browser hand-off. The terminal cannot do WebAuthn, so the
// CLI listens on a loopback port, opens the browser at the hub with that
// port and a state it minted, and waits: the panel, once the person has
// signed in with their passkey, asks them to approve this computer and
// sends the browser back here with a one-time code. The code is traded
// for a session; the session goes in the config file.
func login(ctx context.Context, hub string) error {
	hub = strings.TrimRight(strings.TrimSpace(hub), "/")
	if !strings.Contains(hub, "://") {
		hub = "http://" + hub
	}
	if _, err := url.Parse(hub); err != nil {
		return err
	}
	if _, err := (&http.Client{Timeout: 5 * time.Second}).Get(hub + "/api/health"); err != nil {
		return fmt.Errorf("no hub answers at %s: %w", hub, err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer ln.Close()
	state := randomHex()
	got := make(chan string, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state || q.Get("code") == "" {
			http.Error(w, "this is not the sign-in that was started here", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><title>HomeDash</title><body style=\"font-family:system-ui;padding:3rem\"><p>The command line is signed in. You can close this tab.</p>"))
		select {
		case got <- q.Get("code"):
		default:
		}
	})}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	page := fmt.Sprintf("%s/#cli?port=%d&state=%s", hub, port, state)
	fmt.Fprintf(os.Stderr, "Opening your browser to sign in at %s\nIf it does not open, visit:\n\n  %s\n\n", hub, page)
	openBrowser(page)

	var code string
	select {
	case code = <-got:
	case <-time.After(5 * time.Minute):
		return errors.New("no approval arrived within five minutes")
	case <-ctx.Done():
		return ctx.Err()
	}

	body, _ := json.Marshal(map[string]string{"code": code})
	resp, err := http.Post(hub+"/api/auth/cli/redeem", "application/json", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct{ Token, Name, Role string }
	if resp.StatusCode != http.StatusOK {
		var msg strings.Builder
		_, _ = fmt.Fprint(&msg, resp.Status)
		return errors.New(msg.String())
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	c := &Config{Hub: hub, Token: out.Token, Name: out.Name, Role: out.Role}
	if err := c.Save(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Signed in to %s as %s (%s).\n", hub, out.Name, out.Role)
	return nil
}

// openBrowser hands the URL to the person's own browser — the one their
// passkey lives in — and says nothing if it can't; the URL was printed.
func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		if _, err := exec.LookPath("xdg-open"); err != nil {
			return
		}
		cmd = exec.Command("xdg-open", u)
	}
	_ = cmd.Start()
	go func() { _ = cmd.Wait() }()
}

func randomHex() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
