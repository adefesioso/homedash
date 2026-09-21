// Package notify sends transitions somewhere a person will see them: an
// ntfy topic, or any URL to POST at. Best-effort, never blocking what it
// reports on; there is no per-event picker because every event that gets
// here is a transition into or out of trouble.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Notifier posts to whatever target Settings names at the time.
type Notifier struct {
	// Target returns the current URL, or "" for none. Read per event so a
	// Settings change applies without a restart.
	Target func() string
	Log    *slog.Logger
	queue  chan msg
}

type msg struct{ kind, subject, message string }

// New starts the sender goroutine.
func New(target func() string, log *slog.Logger) *Notifier {
	n := &Notifier{Target: target, Log: log, queue: make(chan msg, 256)}
	go n.run()
	return n
}

// Send queues one transition. A full queue drops rather than blocks.
func (n *Notifier) Send(kind, subject, message string) {
	select {
	case n.queue <- msg{kind, subject, message}:
	default:
		n.Log.Warn("notification dropped, queue full", "kind", kind)
	}
}

func (n *Notifier) run() {
	client := &http.Client{Timeout: 10 * time.Second}
	for m := range n.queue {
		url := strings.TrimSpace(n.Target())
		if url == "" {
			continue
		}
		var req *http.Request
		var err error
		if strings.Contains(url, "ntfy") {
			// ntfy takes the body as the message and headers for the rest.
			req, err = http.NewRequest(http.MethodPost, url, strings.NewReader(m.message))
			if err == nil {
				req.Header.Set("Title", "HomeDash: "+m.subject)
				req.Header.Set("Tags", tag(m.kind))
			}
		} else {
			b, _ := json.Marshal(map[string]string{"kind": m.kind, "subject": m.subject, "message": m.message})
			req, err = http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
			if err == nil {
				req.Header.Set("Content-Type", "application/json")
			}
		}
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		resp, err := client.Do(req.WithContext(ctx))
		cancel()
		if err != nil {
			n.Log.Warn("notification failed", "err", err)
			continue
		}
		resp.Body.Close()
	}
}

// tag is an ntfy emoji hint: trouble or its end.
func tag(kind string) string {
	switch {
	case strings.HasSuffix(kind, ".online"), strings.HasSuffix(kind, ".ok"), strings.HasSuffix(kind, ".recovered"), strings.HasSuffix(kind, ".rebuilt"):
		return "white_check_mark"
	case strings.HasSuffix(kind, ".enrolled"):
		return "tada"
	default:
		return "warning"
	}
}
