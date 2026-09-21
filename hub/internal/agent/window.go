package agent

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
)

// replayBytes is how much of a window's recent output is kept so a
// reattaching browser gets the last screenful back.
const replayBytes = 64 << 10

// Window is one live omp process in a PTY. It outlives any WebSocket
// attached to it: closing the browser detaches, reopening reattaches.
type Window struct {
	ID    int64
	agent *Agent
	cmd   *exec.Cmd
	tty   *os.File

	mu      sync.Mutex
	recent  []byte
	subs    map[chan []byte]struct{}
	touched time.Time
	done    chan struct{}
}

// startWindow launches omp interactively with no tools of its own: its
// only tools come from the hub's MCP server named in agent/mcp.json.
func (a *Agent) startWindow(id int64, model string) (*Window, error) {
	args := []string{"--no-tools", "--cwd", a.windowsDir()}
	if model != "" {
		args = append(args, "--model", model)
	}
	// Warm omp's provider cache before the TUI starts, reusing the same
	// cache Models() serves to the panel's pickers: a window opened
	// within modelsTTL of the last one costs no discovery at all. A
	// stale credential or a pool host that just came online is caught
	// up to on the next expiry, or right away with the panel's "Refresh
	// model cache" button (RefreshModels), not forced on every window.
	_, _ = a.Models(context.Background())
	cmd := exec.Command(a.bin(), args...)
	cmd.Env = a.env()
	cmd.Dir = a.windowsDir()
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 120, Rows: 36})
	if err != nil {
		return nil, err
	}
	w := &Window{ID: id, agent: a, cmd: cmd, tty: tty, subs: map[chan []byte]struct{}{}, done: make(chan struct{})}
	go w.pump()
	return w, nil
}

// pump copies the PTY's output into the replay buffer and to every
// attached socket, then records the end when the process exits.
func (w *Window) pump() {
	buf := make([]byte, 32<<10)
	for {
		n, err := w.tty.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			w.mu.Lock()
			w.recent = append(w.recent, chunk...)
			if len(w.recent) > replayBytes {
				w.recent = w.recent[len(w.recent)-replayBytes:]
			}
			for ch := range w.subs {
				select {
				case ch <- chunk:
				default:
					// A reader this far behind is not a terminal anyone is
					// looking at; drop it rather than stall the window.
					delete(w.subs, ch)
					close(ch)
				}
			}
			w.mu.Unlock()
		}
		if err != nil {
			break
		}
	}
	_ = w.cmd.Wait()
	_ = w.tty.Close()
	w.mu.Lock()
	for ch := range w.subs {
		delete(w.subs, ch)
		close(ch)
	}
	recent := w.recent
	w.recent = nil
	w.mu.Unlock()
	close(w.done)
	w.agent.ended(w.ID, recent)
}

// Attach returns the recent output to replay and a channel of everything
// after it. The channel closes when the window ends or the reader falls
// too far behind.
func (w *Window) Attach() ([]byte, chan []byte) {
	ch := make(chan []byte, 256)
	w.mu.Lock()
	defer w.mu.Unlock()
	w.subs[ch] = struct{}{}
	return append([]byte(nil), w.recent...), ch
}

// Detach stops delivery to ch.
func (w *Window) Detach(ch chan []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.subs[ch]; ok {
		delete(w.subs, ch)
		close(ch)
	}
}

// Write is keyboard input. It counts as activity, recorded at most every
// few seconds so a typist does not become a write per keystroke.
func (w *Window) Write(p []byte) error {
	_, err := w.tty.Write(p)
	w.mu.Lock()
	touch := time.Since(w.touched) > 5*time.Second
	if touch {
		w.touched = time.Now()
	}
	w.mu.Unlock()
	if touch {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = w.agent.Store.TouchWindow(ctx, w.ID)
	}
	return err
}

// Resize follows the browser's terminal size.
func (w *Window) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 {
		return nil
	}
	return pty.Setsize(w.tty, &pty.Winsize{Cols: cols, Rows: rows})
}

// kill ends the process; pump records the rest.
func (w *Window) kill() {
	if w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
	}
	select {
	case <-w.done:
	case <-time.After(5 * time.Second):
	}
}
