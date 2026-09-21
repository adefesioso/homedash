package main

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"syscall"
	"time"
)

// open is the desktop launcher. It waits briefly for the local hub to
// answer, then opens the panel as an app-style window in the user's
// browser. There is no embedded web view on purpose: passkeys work in the
// browser on localhost and do not reliably work in WebKitGTK.
func open(log *slog.Logger) error {
	addr := envOr("HOMEDASH_ADDR", defaultAddr)
	// The page is opened on localhost, not 127.0.0.1: passkeys are bound
	// to a host name, and localhost is the one the hub registers them for.
	_, port, _ := net.SplitHostPort(addr)
	url := "http://localhost:" + port + "/"

	if err := waitHealthy(url+"api/health", 5*time.Second); err != nil {
		return fmt.Errorf("the hub is not running on %s (is the homedash service enabled?): %w", addr, err)
	}
	for _, c := range launchers(url) {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		// Fully detached: the browser outlives the launcher and must not
		// hold its stdio open.
		cmd := exec.Command(c[0], c[1:]...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err == nil {
			log.Info("opened panel", "with", c[0], "url", url)
			return cmd.Process.Release()
		}
	}
	return fmt.Errorf("no browser found to open %s", url)
}

// launchers is the preference order: a Chromium-family app window first,
// then Firefox in its own window, then whatever xdg-open chooses.
func launchers(url string) [][]string {
	return [][]string{
		{"chromium", "--app=" + url},
		{"chromium-browser", "--app=" + url},
		{"google-chrome", "--app=" + url},
		{"brave-browser", "--app=" + url},
		{"microsoft-edge", "--app=" + url},
		{"firefox", "--new-window", url},
		{"xdg-open", url},
	}
}

func waitHealthy(url string, within time.Duration) error {
	deadline := time.Now().Add(within)
	client := &http.Client{Timeout: time.Second}
	var last error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			last = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			last = err
		}
		time.Sleep(250 * time.Millisecond)
	}
	return last
}
