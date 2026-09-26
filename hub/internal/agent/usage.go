package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adefesioso/homedash/hub/internal/store"
)

// sessionsDir is where the hub's own omp writes a window's session, one
// JSONL file per session.
func (a *Agent) sessionsDir() string { return filepath.Join(a.root(), "agent", "sessions") }

// runUsage counts the hub agent's replies once a minute. A window is an
// interactive TUI, so there is no event stream to read as a job's is;
// omp's session file carries the same per-reply usage instead.
func (a *Agent) runUsage(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		if err := a.ScanUsage(ctx); err != nil && ctx.Err() == nil {
			a.log.Warn("hub usage scan", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// ScanUsage reads every session file past the offset already counted,
// complete lines only, and records each assistant reply's usage.
func (a *Agent) ScanUsage(ctx context.Context) error {
	return filepath.WalkDir(a.sessionsDir(), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") || ctx.Err() != nil {
			return ctx.Err()
		}
		return a.scanSession(ctx, path)
	})
}

func (a *Agent) scanSession(ctx context.Context, path string) error {
	off, err := a.Store.HubUsageOffset(ctx, path)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() < off {
		off = 0 // rewritten from scratch: count it again from the top
	}
	if st.Size() == off {
		return nil
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return err
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	// Only whole lines: omp may be mid-write on the last one.
	end := bytes.LastIndexByte(b, '\n')
	if end < 0 {
		return nil
	}
	return a.Store.RecordHubUsage(ctx, path, off+int64(end)+1, parseSessionUsage(b[:end]))
}

// parseSessionUsage is every assistant reply in a stretch of a session
// file that carries usage — the same counters a job's `message_end` does.
func parseSessionUsage(b []byte) []store.Usage {
	var out []store.Usage
	for _, line := range bytes.Split(b, []byte{'\n'}) {
		if !bytes.Contains(line, []byte(`"usage"`)) {
			continue
		}
		var ev struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Message   *struct {
				Role     string `json:"role"`
				Provider string `json:"provider"`
				Model    string `json:"model"`
				Usage    *struct {
					Input      int64 `json:"input"`
					Output     int64 `json:"output"`
					CacheRead  int64 `json:"cacheRead"`
					CacheWrite int64 `json:"cacheWrite"`
					Cost       struct {
						Total float64 `json:"total"`
					} `json:"cost"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &ev) != nil || ev.Type != "message" || ev.Message == nil || ev.Message.Role != "assistant" || ev.Message.Usage == nil {
			continue
		}
		at := ev.Timestamp
		if _, err := time.Parse(time.RFC3339Nano, at); err != nil {
			at = time.Now().UTC().Format(time.RFC3339Nano)
		}
		model := ev.Message.Model
		if ev.Message.Provider != "" {
			model = ev.Message.Provider + "/" + model
		}
		u := ev.Message.Usage
		out = append(out, store.Usage{At: at, Model: model, Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite, Cost: u.Cost.Total})
	}
	return out
}
