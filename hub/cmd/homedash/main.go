// homedash is one binary with two doors and a CLI: `homedash serve` is the
// hub, the always-on process the systemd unit runs; `homedash` alone is
// the desktop launcher that opens the panel against the local hub; and any
// other subcommand is the CLI, the fleet from a workstation signed in as
// a person.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/adefesioso/homedash/hub/internal/agent"
	"github.com/adefesioso/homedash/hub/internal/apps"
	"github.com/adefesioso/homedash/hub/internal/auth"
	"github.com/adefesioso/homedash/hub/internal/backup"
	"github.com/adefesioso/homedash/hub/internal/cli"
	"github.com/adefesioso/homedash/hub/internal/fleet"
	"github.com/adefesioso/homedash/hub/internal/identity"
	"github.com/adefesioso/homedash/hub/internal/notify"
	"github.com/adefesioso/homedash/hub/internal/peers"
	"github.com/adefesioso/homedash/hub/internal/pool"
	"github.com/adefesioso/homedash/hub/internal/remote"
	"github.com/adefesioso/homedash/hub/internal/server"
	"github.com/adefesioso/homedash/hub/internal/storage"
	"github.com/adefesioso/homedash/hub/internal/store"
	"github.com/adefesioso/homedash/hub/internal/tasks"
	"github.com/adefesioso/homedash/hub/ui"
)

// version is stamped by the Makefile with -ldflags.
var version = "dev"

const (
	defaultAddr       = "127.0.0.1:7433"
	defaultEnrollAddr = ":7434"
	defaultState      = "/var/lib/homedash"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cmd := "open"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve(log)
	case "open":
		err = open(log)
	case "recover", "invite", "token":
		err = invite(cmd, os.Args[2:])
	case "export", "restore":
		err = exportRestore(cmd, os.Args[2:])
	case "version":
		fmt.Println(version)
	default:
		// The CLI: its own usage, its own error voice.
		if err := cli.Run(os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "homedash:", err)
			os.Exit(1)
		}
		return
	}
	if err != nil {
		log.Error(cmd, "err", err)
		os.Exit(1)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func serve(log *slog.Logger) error {
	addr := envOr("HOMEDASH_ADDR", defaultAddr)
	stateDir := envOr("HOMEDASH_STATE", defaultState)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("state dir %s: %w", stateDir, err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// A restore the panel staged is applied here, before anything has
	// the state open; the event is recorded once the store is.
	restored, err := backup.Apply(stateDir)
	if err != nil {
		return err
	}
	key, err := identity.Load(stateDir)
	if err != nil {
		return err
	}
	st, err := store.Open(ctx, stateDir)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.RecordEvent(ctx, "hub.start", "", "hub "+version+" started"); err != nil {
		return err
	}
	if restored {
		_ = st.RecordEvent(ctx, "backup.restored", "backup", "the state directory was restored from an export")
	}

	// A transition is recorded and sent on, never blocking what reported it.
	notifier := notify.New(func() string { v, _ := st.Setting(context.Background(), "notify.target"); return v }, log)
	notifyFn := func(kind, subject, message string) {
		nctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := st.RecordEvent(nctx, kind, subject, message); err != nil {
			log.Error("record event", "kind", kind, "err", err)
		}
		notifier.Send(kind, subject, message)
	}

	// The hub's own omp: pinned binary, config under the state dir, the
	// vault as a child. Its only tools are the hub's MCP server, reached
	// over the same listener as the panel — with a token of its own, made
	// fresh on every start, so the window is a client like any other.
	_ = st.DeleteToken(ctx, "hub-agent")
	agentToken, err := st.NewToken(ctx, "hub-agent", "admin")
	if err != nil {
		return err
	}
	ag := agent.New(st, stateDir, "http://"+addr+"/api/mcp", addr, agentToken, notifyFn, log)
	if err := ag.Start(ctx); err != nil {
		return err
	}

	enrollAddr := envOr("HOMEDASH_ENROLL_ADDR", defaultEnrollAddr)
	exec := &remote.Executor{Signer: key.Signer, Timeout: 10 * time.Second}
	defer exec.Close()
	fl := &fleet.Fleet{
		Store:  st,
		Exec:   exec,
		PubKey: key.AuthorizedKey,
		EnrollURL: func() string {
			lan, _ := st.Setting(context.Background(), "hub.lan_addr")
			if lan == "" {
				lan = lanAddress()
			}
			_, port, _ := net.SplitHostPort(enrollAddr)
			return "http://" + net.JoinHostPort(lan, port)
		},
		OmpRelease:    agent.OmpRelease,
		LlmfitVersion: agent.LlmfitVersion, LlmfitRelease: agent.LlmfitRelease,
		VaultToken: ag.VaultToken, VaultAddr: agent.VaultAddr,
		Notify: notifyFn, Log: log,
	}
	go fl.Heartbeat(ctx)
	go fl.ScanLoop(ctx)
	jobs := &agent.Jobs{Store: st, Fleet: fl, Agent: ag, Notify: notifyFn}
	pl := pool.New(st, fl, notifyFn, log)
	fl.Router = pl.Serve()
	pe := &peers.Peers{Store: st, Pool: pl, Fleet: fl, StateDir: stateDir, Notify: notifyFn, Log: log, Router: fl.Router}
	pl.Peers = pe
	go pe.Run(ctx)
	ts := tasks.New(st, fl, notifyFn, log)
	ap := &apps.Apps{Store: st, Fleet: fl, Notify: notifyFn}
	sto := &storage.Storage{Store: st, Fleet: fl, Notify: notifyFn}
	bk := &backup.Backup{Store: st, StateDir: stateDir}
	if err := ts.Start(ctx); err != nil {
		return err
	}

	// Sign-in: passkeys bound to the host the panel is opened on. The
	// launcher always opens localhost, so that origin is always valid;
	// when the hub is also reachable at a LAN hostname (bound to that
	// name directly, or to 0.0.0.0/:: for every interface with a
	// resolvable name such as mDNS's <host>.local set in hub.lan_addr),
	// that name is registered too, so passkeys work when the panel is
	// opened straight from a browser instead of the launcher. WebAuthn's
	// RP ID must be a domain, never an IP literal, so a bare address
	// (the usual case for hub.lan_addr, worked out from the default
	// route) is never registered here; it would make the hub refuse to
	// start rather than let passkeys silently not work.
	bindHost, port, _ := net.SplitHostPort(addr)
	rpID, _ := st.Setting(ctx, "auth.rpid")
	origins := []string{"http://localhost:" + port}
	lanHost := bindHost
	switch lanHost {
	case "0.0.0.0", "::", "", "127.0.0.1", "localhost", "::1":
		lanHost, _ = st.Setting(ctx, "hub.lan_addr")
	}
	if lanHost != "" && net.ParseIP(lanHost) == nil {
		origins = append(origins, "http://"+net.JoinHostPort(lanHost, port))
		if rpID == "" {
			rpID = lanHost
		}
	}
	if rpID == "" {
		rpID = "localhost"
	}
	if extra, _ := st.Setting(ctx, "auth.origins"); extra != "" {
		for _, o := range strings.Split(extra, ",") {
			if o = strings.TrimSpace(o); o != "" {
				origins = append(origins, o)
			}
		}
	}
	au, err := auth.New(st, rpID, origins)
	if err != nil {
		return err
	}

	dist, err := fs.Sub(ui.Dist, "dist")
	if err != nil {
		return err
	}
	s := server.New(st, key, ag, fl, jobs, pl, ts, ap, sto, bk, au, pe, notifyFn, dist, version, log)
	// A restart is a shutdown that exits non-zero, so the unit's
	// Restart=on-failure brings the hub back with the staged restore
	// applied. Outside systemd, whoever runs it starts it again.
	var restartReason string
	s.Restart = func(reason string) {
		time.Sleep(200 * time.Millisecond) // let the reply leave first
		restartReason = reason
		stop()
	}
	// An ingress front listens on :443/:80 only while a hostname is mapped.
	go s.Ingress(ctx, stateDir)
	srv := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	esrv := &http.Server{Addr: enrollAddr, Handler: s.Enrollment(), ReadHeaderTimeout: 10 * time.Second}
	eln, err := net.Listen("tcp", enrollAddr)
	if err != nil {
		return err
	}
	log.Info("hub listening", "addr", addr, "enroll", enrollAddr, "state", filepath.Clean(stateDir), "version", version)

	errc := make(chan error, 2)
	go func() { errc <- srv.Serve(ln) }()
	go func() { errc <- esrv.Serve(eln) }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = st.RecordEvent(shutdownCtx, "hub.stop", "", "hub stopping")
		err := srv.Shutdown(shutdownCtx)
		_ = esrv.Shutdown(shutdownCtx)
		// ctx is done, so the vault is on its way out; the windows go with
		// it, and each records its end as it goes.
		ag.Stop()
		if restartReason != "" {
			return fmt.Errorf("restarting: %s", restartReason)
		}
		return err
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// lanAddress is the address this machine uses to reach the network: what
// a remote on the LAN can fetch the enrollment script from.
func lanAddress() string {
	c, err := net.Dial("udp", "192.0.2.1:1")
	if err != nil {
		return "127.0.0.1"
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP.String()
}

// invite is the shell's authority: `homedash recover` prints a one-time
// admin registration code, `homedash invite [admin|viewer]` a code for a
// new account, `homedash token NAME` an admin API token for a script.
// Run as the hub's user: sudo -u homedash homedash recover.
func invite(cmd string, args []string) error {
	stateDir := envOr("HOMEDASH_STATE", defaultState)
	ctx := context.Background()
	st, err := store.Open(ctx, stateDir)
	if err != nil {
		return err
	}
	defer st.Close()
	if cmd == "token" {
		if len(args) == 0 {
			return fmt.Errorf("token needs a name")
		}
		// A name is one live token (tokens_name, C-6): replace rather
		// than refuse, the same way the hub replaces its own hub-agent
		// token on every start, so re-running this for the same name
		// (tests/lib.sh's fixed "tests-suite", a rotated script token)
		// just mints a new one instead of erroring.
		name := args[0]
		_ = st.DeleteToken(ctx, name)
		tok, err := st.NewToken(ctx, name, "admin")
		if err != nil {
			return err
		}
		_ = st.RecordEvent(ctx, "auth.token", name, "an admin API token named "+name+" was made from a shell on the hub")
		fmt.Println(tok)
		return nil
	}
	role := "admin"
	if cmd == "invite" {
		role = "viewer"
		if len(args) > 0 {
			role = args[0]
		}
		if role != "admin" && role != "viewer" {
			return fmt.Errorf("role is admin or viewer")
		}
	}
	inv, err := st.NewInvite(ctx, role, nil)
	if err != nil {
		return err
	}
	_ = st.RecordEvent(ctx, "auth.invite", role, "a one-time "+role+" registration code was printed from a shell on the hub")
	fmt.Printf("One-time %s registration code (good for a day, once):\n\n  %s\n\nOpen the panel, choose Register, and paste it.\n", role, inv.Code)
	return nil
}

// exportRestore is the shell's export and restore: `homedash export FILE`
// writes the state directory as one passphrase-encrypted file (the hub
// may be running), `homedash restore FILE` puts one back, with the hub
// stopped. The passphrase is asked on the terminal, or taken from
// HOMEDASH_PASSPHRASE for a script. Run as the hub's user.
func exportRestore(cmd string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s needs a file", cmd)
	}
	stateDir := envOr("HOMEDASH_STATE", defaultState)
	file := args[0]
	ctx := context.Background()
	if cmd == "export" {
		pass, err := passphrase(true)
		if err != nil {
			return err
		}
		st, err := store.Open(ctx, stateDir)
		if err != nil {
			return err
		}
		defer st.Close()
		f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		bk := &backup.Backup{Store: st, StateDir: stateDir}
		if err := bk.Export(ctx, f, pass); err != nil {
			f.Close()
			os.Remove(file)
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		_ = st.RecordEvent(ctx, "backup.export", "backup", "the state directory was exported from a shell on the hub")
		fmt.Println("exported to", file)
		return nil
	}
	// A restore swaps the database under whatever has it open: refuse
	// while the hub answers on its port.
	addr := envOr("HOMEDASH_ADDR", defaultAddr)
	c := &http.Client{Timeout: 2 * time.Second}
	if r, err := c.Get("http://" + addr + "/api/health"); err == nil {
		r.Body.Close()
		return fmt.Errorf("the hub is running on %s; stop it first (systemctl stop homedash)", addr)
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	pass, err := passphrase(false)
	if err != nil {
		return err
	}
	if err := backup.Stage(stateDir, f, pass); err != nil {
		return err
	}
	if _, err := backup.Apply(stateDir); err != nil {
		return err
	}
	st, err := store.Open(ctx, stateDir)
	if err != nil {
		return err
	}
	defer st.Close()
	_ = st.RecordEvent(ctx, "backup.restored", "backup", "the state directory was restored from an export on a shell on the hub")
	fmt.Println("restored from", file, "— start the hub (systemctl start homedash)")
	return nil
}

// passphrase reads one without echo, twice for a new export so a typo
// does not make a file nothing opens; HOMEDASH_PASSPHRASE skips the prompt.
func passphrase(confirm bool) (string, error) {
	if p := os.Getenv("HOMEDASH_PASSPHRASE"); p != "" {
		return p, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("no terminal to ask the passphrase on; set HOMEDASH_PASSPHRASE")
	}
	ask := func(prompt string) (string, error) {
		fmt.Fprint(os.Stderr, prompt)
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	p, err := ask("Passphrase: ")
	if err != nil {
		return "", err
	}
	if p == "" {
		return "", errors.New("a passphrase is needed")
	}
	if confirm {
		again, err := ask("Again: ")
		if err != nil {
			return "", err
		}
		if again != p {
			return "", errors.New("the passphrases differ")
		}
	}
	return p, nil
}
