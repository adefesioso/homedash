package pool

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/adefesioso/homedash/hub/internal/store"
)

// CallerHeader names who asked the router, set by whichever door the
// request came through — the hub's listener, the job door, a peer's
// stream — and never taken from the request itself: each door overwrites
// it. Empty is a caller that came in some other way.
const CallerHeader = "X-HomeDash-Caller"

// maxLine is the longest answer line the meter reads for counts; a
// longer one (an embedding's vectors) is passed through unread.
const maxLine = 1 << 20

// meter watches an answer stream by for the token counts in it, in
// whichever of the router's shapes it arrives: Ollama's final
// `done` object (prompt_eval_count, eval_count), OpenAI's `usage`
// (prompt_tokens, completion_tokens), plain or as SSE `data:` lines.
// The last counts seen win — the final chunk is the one that carries
// them.
type meter struct {
	r             io.Reader
	line          []byte
	skip          bool
	input, output int64
}

func (m *meter) Read(p []byte) (int, error) {
	n, err := m.r.Read(p)
	for _, c := range p[:n] {
		if c == '\n' {
			m.flush()
			continue
		}
		if m.skip {
			continue
		}
		if len(m.line) >= maxLine {
			m.line, m.skip = m.line[:0], true
			continue
		}
		m.line = append(m.line, c)
	}
	if err != nil {
		m.flush()
	}
	return n, err
}

func (m *meter) flush() {
	line := bytes.TrimSpace(m.line)
	m.line, m.skip = m.line[:0], false
	line = bytes.TrimPrefix(line, []byte("data:"))
	if !bytes.Contains(line, []byte("_count")) && !bytes.Contains(line, []byte(`"usage"`)) {
		return
	}
	var v struct {
		PromptEval int64 `json:"prompt_eval_count"`
		Eval       int64 `json:"eval_count"`
		Usage      *struct {
			Prompt     int64 `json:"prompt_tokens"`
			Completion int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(bytes.TrimSpace(line), &v) != nil {
		return
	}
	switch {
	case v.Usage != nil && v.Usage.Prompt+v.Usage.Completion > 0:
		m.input, m.output = v.Usage.Prompt, v.Usage.Completion
	case v.PromptEval+v.Eval > 0:
		m.input, m.output = v.PromptEval, v.Eval
	}
}

// record keeps what the meter saw as served by server, for whoever the
// request's door said asked. An answer that carried no counts (an
// error, a stream cut short) is still one request served, at zero.
func (p *Pool) record(r *http.Request, m *meter, server string, peer bool, model string) {
	if p.Store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Store.RecordServed(ctx, store.Served{
		Server: server, Peer: peer, Caller: r.Header.Get(CallerHeader), Model: model, Input: m.input, Output: m.output,
	}); err != nil && p.Log != nil {
		p.Log.Warn("served usage", "err", err)
	}
}
