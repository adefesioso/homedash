// Package remote is the one door to a remote. Every command the hub runs
// on a machine goes through Run: the connection is pinned to the host key
// enrollment recorded, every argument is quoted, never concatenated, and a
// script travels on stdin rather than in the command line.
package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Target is what a connection needs: the pinned key decides whether the
// machine on the other end is the one enrollment met.
type Target struct {
	Addr    string // host:port
	User    string
	HostKey string // authorized_keys form, as enrollment reported it
}

// ErrKeyMismatch is a machine presenting a host key other than the pinned
// one. It is refused before authentication and surfaced, never trusted.
var ErrKeyMismatch = errors.New("host key mismatch")

// Result is what a command left behind.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Duration time.Duration
}

// Executor holds the hub's key and dials with it.
type Executor struct {
	Signer ssh.Signer
	// Timeout bounds the dial and handshake, not the command.
	Timeout time.Duration

	// The kept connections: a handshake is the expensive part of every
	// command, so Run and Sh reuse one client per target. Dial is not
	// cached — its caller owns the connection it gets.
	mu   sync.Mutex
	kept map[string]*kept
}

type kept struct {
	client *ssh.Client
	used   time.Time
	timer  *time.Timer
}

// keptIdle is how long an unused connection stays open.
const keptIdle = 2 * time.Minute

func (t Target) key() string { return t.User + "@" + t.Addr + "\n" + t.HostKey }

// client returns a kept connection to t, dialing when there is none;
// reused says which.
func (e *Executor) client(ctx context.Context, t Target) (c *ssh.Client, reused bool, err error) {
	key := t.key()
	e.mu.Lock()
	k := e.kept[key]
	if k != nil {
		k.used = time.Now()
		k.timer.Reset(keptIdle)
		e.mu.Unlock()
		return k.client, true, nil
	}
	e.mu.Unlock()
	c, err = e.Dial(ctx, t)
	if err != nil {
		return nil, false, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.kept == nil {
		e.kept = map[string]*kept{}
	}
	if other := e.kept[key]; other != nil {
		// Two callers dialed at once; keep the first, use this one once.
		go func() { time.Sleep(keptIdle); c.Close() }()
		return c, false, nil
	}
	k = &kept{client: c, used: time.Now()}
	k.timer = time.AfterFunc(keptIdle, func() { e.drop(key, c) })
	e.kept[key] = k
	// A connection the server closes is found out here, not by the next
	// command failing on it.
	go func() { _ = c.Wait(); e.drop(key, c) }()
	return c, false, nil
}

// drop forgets a kept connection and closes it, if it is still the one
// kept under key.
func (e *Executor) drop(key string, c *ssh.Client) {
	e.mu.Lock()
	k := e.kept[key]
	if k != nil && k.client == c {
		delete(e.kept, key)
		k.timer.Stop()
	}
	e.mu.Unlock()
	c.Close()
}

// Close closes every kept connection.
func (e *Executor) Close() {
	e.mu.Lock()
	kept := e.kept
	e.kept = nil
	e.mu.Unlock()
	for _, k := range kept {
		k.timer.Stop()
		k.client.Close()
	}
}

// Dial opens a pinned connection. The caller closes it.
func (e *Executor) Dial(ctx context.Context, t Target) (*ssh.Client, error) {
	pinned, _, _, _, err := ssh.ParseAuthorizedKey([]byte(t.HostKey))
	if err != nil {
		return nil, fmt.Errorf("pinned host key: %w", err)
	}
	cfg := &ssh.ClientConfig{
		User: t.User,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(e.Signer)},
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if !bytes.Equal(key.Marshal(), pinned.Marshal()) {
				return ErrKeyMismatch
			}
			return nil
		},
		// Ask for the pinned key's own algorithm, so the server presents
		// the key enrollment recorded rather than another of its keys.
		HostKeyAlgorithms: []string{pinned.Type()},
		Timeout:           e.Timeout,
	}
	d := net.Dialer{Timeout: e.Timeout}
	conn, err := d.DialContext(ctx, "tcp", t.Addr)
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, t.Addr, cfg)
	if err != nil {
		conn.Close()
		if strings.Contains(err.Error(), ErrKeyMismatch.Error()) {
			return nil, ErrKeyMismatch
		}
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// Run executes argv on the target with stdin, and returns when it exits or
// ctx ends. argv[0] is the program; nothing is interpreted by a shell
// unless argv itself is a shell, in which case the script is on stdin.
func (e *Executor) Run(ctx context.Context, t Target, argv []string, stdin io.Reader) (*Result, error) {
	c, reused, err := e.client(ctx, t)
	if err != nil {
		return nil, err
	}
	r, err := RunOn(ctx, c, argv, stdin)
	if err != nil {
		// Not the command failing — that is an exit code — so either the
		// connection itself broke or it hung until the caller gave up,
		// which on a machine that was unplugged looks the same from here.
		// Drop it so the next call dials fresh.
		e.drop(t.key(), c)
		// A kept connection the server had already closed fails before
		// the command starts; that is worth one fresh dial rather than a
		// false "offline". Nothing ran, so nothing is run twice.
		if reused && errors.Is(err, errSession) && ctx.Err() == nil {
			return e.Run(ctx, t, argv, stdin)
		}
	}
	return r, err
}

// errSession is a session that could not be opened on a connection —
// the command never started.
var errSession = errors.New("open session")

// RunOn is Run on an open connection, for callers holding one across
// several commands (a job with its reverse forward up).
func RunOn(ctx context.Context, c *ssh.Client, argv []string, stdin io.Reader) (*Result, error) {
	if len(argv) == 0 {
		return nil, errors.New("empty command")
	}
	s, err := c.NewSession()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errSession, err)
	}
	defer s.Close()
	var out, errb bytes.Buffer
	s.Stdout, s.Stderr, s.Stdin = &out, &errb, stdin
	start := time.Now()
	if err := s.Start(Quote(argv)); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- s.Wait() }()
	select {
	case <-ctx.Done():
		_ = s.Signal(ssh.SIGKILL)
		return nil, ctx.Err()
	case err = <-done:
	}
	r := &Result{Stdout: out.Bytes(), Stderr: errb.Bytes(), Duration: time.Since(start)}
	var ee *ssh.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		r.ExitCode = ee.ExitStatus()
	default:
		return nil, err
	}
	return r, nil
}

// Sh runs a script through `sh -s` with its text on stdin and the given
// positional arguments, so nothing in the script or the arguments is ever
// part of a command line.
func (e *Executor) Sh(ctx context.Context, t Target, script string, args ...string) (*Result, error) {
	return e.Run(ctx, t, append([]string{"sh", "-s", "--"}, args...), strings.NewReader(script))
}

// Quote renders argv as one POSIX shell command line where every word is
// single-quoted, the only quoting that is safe for arbitrary bytes.
func Quote(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(parts, " ")
}

// Forward opens a reverse forward on c: connections to remoteAddr on the
// remote are carried back and dialed to localAddr here. This is how a
// remote reaches the hub's vault during a job — an address on the remote's
// loopback that only exists while the hub holds this connection. Returns
// a stop function.
func Forward(ctx context.Context, c *ssh.Client, remoteAddr, localAddr string) (func(), error) {
	ln, err := c.Listen("tcp", remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("reverse forward %s: %w", remoteAddr, err)
	}
	go func() {
		for {
			rc, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer rc.Close()
				lc, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", localAddr)
				if err != nil {
					return
				}
				defer lc.Close()
				done := make(chan struct{}, 2)
				go func() { _, _ = io.Copy(lc, rc); done <- struct{}{} }()
				go func() { _, _ = io.Copy(rc, lc); done <- struct{}{} }()
				<-done
			}()
		}
	}()
	return func() { ln.Close() }, nil
}

// WriteFile puts content at path on the connection, mode as given, through
// the account's own shell: no scp, no sftp subsystem to depend on.
func WriteFile(ctx context.Context, c *ssh.Client, path string, content []byte, mode string, sudo bool) error {
	script := `set -e; p="$1"; m="$2"; d=$(dirname "$p"); mkdir -p "$d"; t=$(mktemp "$d/.homedash.XXXXXX"); cat > "$t"; chmod "$m" "$t"; mv -f "$t" "$p"`
	argv := []string{"sh", "-c", script, "sh", path, mode}
	if sudo {
		argv = append([]string{"sudo", "-n"}, argv...)
	}
	r, err := RunOn(ctx, c, argv, bytes.NewReader(content))
	if err != nil {
		return err
	}
	if r.ExitCode != 0 {
		return fmt.Errorf("write %s: %s", path, strings.TrimSpace(string(r.Stderr)))
	}
	return nil
}
