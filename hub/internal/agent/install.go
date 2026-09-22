// Package agent is the hub's side of omp: the pinned binary, the config
// files under the state dir, the credential vault as a child process, and
// the windows a person types into. Nothing here runs a tool on the hub; a
// window's only tools are the hub's own MCP server.
package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// binPath is where the pinned omp lives under the omp root.
func binPath(root string) string { return filepath.Join(root, "bin", "omp") }

// installed reports whether bin/omp is present and runs. Whether it is
// current is not this hub's call any more — see selfUpdate.
func installed(ctx context.Context, root string) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := exec.CommandContext(ctx, binPath(root), "--version").Output()
	return err == nil
}

// version reads the installed binary's own version string ("" if it
// isn't there or won't run), for Status to show.
func version(ctx context.Context, root string) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binPath(root), "--version").Output()
	if err != nil {
		return ""
	}
	// `omp --version` prints "omp/<version>".
	return strings.TrimPrefix(strings.TrimSpace(string(out)), "omp/")
}

// install fetches the latest release asset for this architecture, checks
// it against the release's SHA256SUMS.txt, and moves it into place
// atomically. It's the bootstrap for a machine with no omp binary yet;
// once one exists, selfUpdate is how it stays current.
func install(ctx context.Context, root string) error {
	var asset string
	switch runtime.GOARCH {
	case "amd64":
		asset = "omp-linux-x64"
	case "arm64":
		asset = "omp-linux-arm64"
	default:
		return fmt.Errorf("no omp build for %s", runtime.GOARCH)
	}
	if err := os.MkdirAll(filepath.Dir(binPath(root)), 0o700); err != nil {
		return err
	}
	sums, err := fetch(ctx, OmpRelease+"SHA256SUMS.txt")
	if err != nil {
		return fmt.Errorf("omp checksums: %w", err)
	}
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == asset {
			want = f[0]
		}
	}
	if want == "" {
		return fmt.Errorf("omp checksums: no entry for %s", asset)
	}
	body, err := fetch(ctx, OmpRelease+asset)
	if err != nil {
		return fmt.Errorf("omp %s: %w", asset, err)
	}
	sum := sha256.Sum256(body)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("omp %s: checksum %s, want %s", asset, got, want)
	}
	tmp := binPath(root) + ".part"
	if err := os.WriteFile(tmp, body, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, binPath(root))
}

// selfUpdate runs the installed binary's own updater: it checks GitHub's
// latest release itself and replaces itself in place if newer. This is
// what both "Update oh-my-pi" (the hub) and "Update omp on network"
// (each remote) actually run, once a binary exists to ask.
func selfUpdate(ctx context.Context, root string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, binPath(root), "update", "--force").CombinedOutput()
	if err != nil {
		return fmt.Errorf("omp update: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 512<<20))
}
