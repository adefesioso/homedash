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

// installed reports whether bin/omp is present and is the pinned version.
func installed(ctx context.Context, root string) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binPath(root), "--version").Output()
	if err != nil {
		return false
	}
	// `omp --version` prints "omp/18.1.21".
	got := strings.TrimSpace(string(out))
	return got == "omp/"+strings.TrimPrefix(OmpVersion, "v")
}

// install fetches the pinned release asset for this architecture, checks it
// against the release's SHA256SUMS.txt, and moves it into place atomically.
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
	sums, err := fetch(ctx, ompRelease+"SHA256SUMS.txt")
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
	body, err := fetch(ctx, ompRelease+asset)
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
