package agent

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/adefesioso/homedash/hub/internal/fleet"
	"github.com/adefesioso/homedash/hub/internal/gate"
	"github.com/adefesioso/homedash/hub/internal/remote"
	"github.com/adefesioso/homedash/hub/internal/store"
)

//go:embed snapshot.sh
var snapshotScript string

// reportInstruction is prepended to every job's text. The report is what
// the hub's agent reads, and what the rebuild script is folded from.
const reportInstruction = `You are working on this machine alone, on behalf of the HomeDash hub, as the account ` + gate.AgentAccount + `: you are not root and cannot become root. Your home (and whatever is mounted under it — that is your persistent working space), the working directory, and the house's shared storage mounted on this machine are yours to write; the rest of the system is read-only to you, and the house's network is closed to you. You can run docker yourself for the containers your work needs, but not a privileged container, a host namespace, an added capability, or a bind of / or the docker socket. When the work needs root — a package, a service, a file outside your directories, a mount, a data disk to partition or format and add to /etc/fstab (never the disk the system runs from; that is refused) — run ` + "`homedash-sudo COMMAND...`" + `: the hub checks the command, runs it as root, and returns the output; a refusal comes back as an error you must relay, not route around. Do the work below, then end with a section titled "Report" that says: what you changed, what you could not do, and — as shell commands that would make each change again on a fresh Debian, in order — every package installed, file written (inline as a heredoc if small), service enabled, and mount made. If you produced data that should survive a rebuild, name its path under "Data".

`

// agentHome is where the job account keeps everything it owns.
const agentHome = "/home/" + gate.AgentAccount

// memoryFloor is what omp needs to run at all; below it the kernel kills
// it before the first token.
const memoryFloor = 1400 << 20

// Jobs runs remote jobs. It lives with the agent because a job is omp on
// a remote, driven the way the hub drives its own.
type Jobs struct {
	Store  *store.Store
	Fleet  *fleet.Fleet
	Agent  *Agent
	Notify fleet.Notify
}

// OfflineError is a job asked to start on a host that isn't online. The
// HTTP layer turns it into 409, not the 400 an ordinary bad request gets.
type OfflineError struct{ Host string }

func (e *OfflineError) Error() string { return e.Host + " is offline" }

// Start begins a job on a host and returns at once with its id; the
// rounds run in the background and the tools follow them by id.
func (j *Jobs) Start(ctx context.Context, h *store.Host, cwd, text string, windowID int64) (*store.Job, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("a job needs instructions")
	}
	if h.Status != "online" {
		return nil, &OfflineError{Host: h.Name}
	}
	if cwd == "" {
		cwd = agentHome
	}
	if !strings.HasPrefix(cwd, "/") {
		return nil, errors.New("the working directory must be an absolute path")
	}
	if err := gate.Privileged("mkdir -p " + cwd); err != nil {
		return nil, err
	}
	var facts fleet.Facts
	_ = json.Unmarshal(h.Facts, &facts)
	if facts.MemTotal > 0 && facts.MemTotal < memoryFloor {
		return nil, fmt.Errorf("%s has %d MB of memory; the agent needs about 1.5 GB to run at all", h.Name, facts.MemTotal>>20)
	}
	if facts.Agent == nil {
		return nil, fmt.Errorf("%s has not reported an agent; enrollment installs it", h.Name)
	}
	if !facts.Agent.Account {
		return nil, fmt.Errorf("%s has no agent account yet; re-provision it from the card", h.Name)
	}
	// The working directory has to already be there — a job does not
	// create one out of nowhere, and a cwd that turns out missing only
	// after omp starts is a harder failure to notice than one refused
	// up front. One connection carries this check and the round itself,
	// so accepting the job costs no second dial.
	c, err := j.Fleet.Exec.Dial(ctx, fleet.Target(h))
	if err != nil {
		return nil, fmt.Errorf("cannot connect to %s: %w", h.Name, err)
	}
	if r, err := remote.RunOn(ctx, c, []string{"test", "-d", cwd}, nil); err != nil || r.ExitCode != 0 {
		c.Close()
		return nil, fmt.Errorf("%s does not exist on %s", cwd, h.Name)
	}
	model := h.AgentModel
	if model == "" {
		model = j.Fleet.RemoteModel(ctx)
	}
	timeout := settingInt(ctx, j.Store, "jobs.timeout", 1200)
	id, err := j.Store.CreateJob(ctx, h.ID, windowID, cwd, model, text, timeout)
	if err != nil {
		c.Close()
		return nil, err
	}
	job, err := j.Store.Job(ctx, id)
	if err != nil {
		c.Close()
		return nil, err
	}
	prompt := reportInstruction
	if names, _ := j.Store.SecretNamesFor(ctx, h.ID); len(names) > 0 {
		prompt += "Secrets the hub holds for this machine, readable while this job runs with `homedash-secret NAME` (never write one to disk unless the task requires it): " + strings.Join(names, ", ") + "\n\n"
	}
	go j.round(context.Background(), job, h, prompt+text, false, c)
	return job, nil
}

// sharedPaths is the shared storage a job on this host may write: the
// cluster it is the gateway of, and every workspace it is a member of.
// Both are mounted by root and owned by the hub's account; the job's
// account is in that group, so permission is already there and the unit
// only has to leave the paths writable.
func (j *Jobs) sharedPaths(ctx context.Context, hostID int64) []string {
	var out []string
	clusters, _ := j.Store.Clusters(ctx)
	for _, c := range clusters {
		if c.GatewayID == hostID {
			out = append(out, c.Path)
		}
	}
	workspaces, _ := j.Store.Workspaces(ctx, 0)
	for _, w := range workspaces {
		for _, m := range w.Members {
			if m.HostID == hostID {
				out = append(out, w.Path)
				break
			}
		}
	}
	return out
}

// Correct is a follow-up turn on the same remote session, so the remote
// keeps everything it learned. Rounds are capped; at the cap the job is
// marked needs_you rather than sent round again. A needs_you job is
// itself that person deciding: a correction submitted from that state
// is the decision the cap was waiting for, so it runs rather than
// bouncing back to needs_you unanswered. The cap still applies to the
// round after, unless that one also lands on needs_you and gets its
// own answer.
func (j *Jobs) Correct(ctx context.Context, id int64, text string) (*store.Job, error) {
	job, err := j.Store.Job(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.State == "running" {
		return nil, errors.New("the job is still running")
	}
	if job.Session == "" {
		return nil, errors.New("the job never started a session on the remote; start a new job")
	}
	cap := settingInt(ctx, j.Store, "agent.rounds", 3)
	if job.Rounds >= cap && job.State != "needs_you" {
		reason := fmt.Sprintf("reached the round cap (%d); a person has to decide", cap)
		_ = j.Store.EndJob(ctx, id, "needs_you", job.Report, reason)
		j.Notify("job.needs_you", job.Host, fmt.Sprintf("job %d on %s needs you: %s", id, job.Host, reason))
		return j.Store.Job(ctx, id)
	}
	h, err := j.Store.Host(ctx, strconv.FormatInt(job.HostID, 10))
	if err != nil {
		return nil, err
	}
	if err := j.Store.StartRound(ctx, id); err != nil {
		return nil, err
	}
	job, _ = j.Store.Job(ctx, id)
	go j.round(context.Background(), job, h, text, true, nil)
	return job, nil
}

// Clear removes every finished job, then the snapshot each left on its
// remote, best-effort: a host that is offline keeps a stray snapshot
// until the script's next delete pass, which is not worth refusing the
// clear over. Running jobs stay.
func (j *Jobs) Clear(ctx context.Context) (int, error) {
	gone, err := j.Store.ClearJobs(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for hostID, ids := range gone {
		n += len(ids)
		h, err := j.Store.Host(ctx, strconv.FormatInt(hostID, 10))
		if err != nil || h.Status != "online" {
			continue
		}
		c, err := j.Fleet.Exec.Dial(ctx, fleet.Target(h))
		if err != nil {
			continue
		}
		for _, id := range ids {
			_, _ = j.snapshot(ctx, c, "delete", id)
		}
		c.Close()
	}
	return n, nil
}

// round runs one turn of omp on the remote and records every line it
// emits, then the report. c is the connection Start already dialed to
// check the working directory, reused rather than opened twice; a
// follow-up round (Correct) has none yet and dials its own.
func (j *Jobs) round(ctx context.Context, job *store.Job, h *store.Host, prompt string, resume bool, c *ssh.Client) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(job.TimeoutS)*time.Second+2*time.Minute)
	defer cancel()
	end := func(state, report, reason string) {
		_ = j.Store.EndJob(ctx, job.ID, state, report, reason)
		if state == "failed" {
			j.Notify("job.failed", h.Name, fmt.Sprintf("job %d on %s failed: %s", job.ID, h.Name, reason))
		}
		keep := settingInt(ctx, j.Store, "jobs.retention", 20)
		gone, _ := j.Store.TrimJobs(ctx, h.ID, keep)
		for _, id := range gone {
			if c != nil {
				_, _ = j.snapshot(ctx, c, "delete", id)
			}
		}
	}

	if c == nil {
		var err error
		c, err = j.Fleet.Exec.Dial(ctx, fleet.Target(h))
		if err != nil {
			end("failed", "", "cannot connect: "+err.Error())
			return
		}
	}
	defer c.Close()
	// The vault and the router, on the remote's loopback for as long as
	// this connection lasts. A forward that fails is not fatal: omp starts
	// from its encrypted snapshot, and a cloud model needs no router.
	if stop, err := j.Fleet.ForwardsFor(ctx, c, h, job.ID); err == nil {
		defer stop()
	}
	// The way back, before anything changes.
	if kind, err := j.snapshot(ctx, c, "snapshot", job.ID); err == nil {
		_ = j.Store.SetJobSnapshot(ctx, job.ID, kind)
	} else {
		_ = j.Store.SetJobSnapshot(ctx, job.ID, "none")
		_ = j.Store.AppendJobEvent(ctx, job.ID, `{"type":"stderr","text":"snapshot: `+strings.ReplaceAll(err.Error(), `"`, `'`)+`"}`)
	}

	omp := []string{"omp", "--mode", "json", "-p", "--cwd", job.Cwd, "--approval-mode", "yolo",
		"--max-time", strconv.Itoa(job.TimeoutS), "--no-title"}
	if job.Model != "" {
		omp = append(omp, "--model", job.Model)
	}
	if resume {
		omp = append(omp, "--resume", job.Session)
	}
	// omp resolves --model from its provider cache (agent/models.db)
	// before background discovery finishes, and never refreshes that cache
	// when the pool gains a model. So the cache is rebuilt right before the
	// run, with the door up: `omp models` is the priming step.
	inner := `rm -f "$HOME"/.omp/agent/models.db "$HOME"/.omp/agent/models.db-*; omp models >/dev/null 2>&1; exec ` + remote.Quote(omp)
	// The job's account, inside a unit the kernel enforces: no privilege
	// to gain, the system read-only, its own /tmp, a task cap, a clock.
	argv := []string{"sudo", "-n", "systemd-run", "--quiet", "--pipe", "--wait", "--collect",
		"--unit=homedash-job-" + strconv.FormatInt(job.ID, 10) + "-r" + strconv.Itoa(job.Rounds),
		"--uid=" + gate.AgentAccount, "--gid=" + gate.AgentAccount, "--working-directory=" + job.Cwd,
		"--setenv=HOME=" + agentHome, "--setenv=PATH=/usr/local/bin:/usr/bin:/bin", "--setenv=LANG=C.UTF-8",
		"--setenv=OMP_AUTH_BROKER_URL=http://" + j.Fleet.VaultAddr}
	props := []string{"NoNewPrivileges=yes", "ProtectSystem=strict", "ProtectKernelTunables=yes", "ProtectKernelModules=yes",
		"ProtectControlGroups=yes", "ProtectClock=yes", "PrivateTmp=yes", "RestrictSUIDSGID=yes", "TasksMax=512",
		"RuntimeMaxSec=" + strconv.Itoa(job.TimeoutS+60), "ReadWritePaths=" + agentHome}
	if job.Cwd != agentHome && !strings.HasPrefix(job.Cwd, agentHome+"/") {
		props = append(props, "ReadWritePaths="+job.Cwd)
	}
	// The house's shared storage, wherever the job started. The "-" is
	// systemd's: a path that isn't there (a workspace not mounted yet, a
	// cluster whose members are off) is skipped, not a failed unit.
	for _, p := range j.sharedPaths(ctx, h.ID) {
		props = append(props, "ReadWritePaths=-"+p)
	}
	if v, _ := j.Store.Setting(ctx, "jobs.memory_max"); strings.TrimSpace(v) != "" {
		props = append(props, "MemoryMax="+strings.TrimSpace(v))
	}
	if v, _ := j.Store.Setting(ctx, "jobs.cpu_quota"); strings.TrimSpace(v) != "" {
		props = append(props, "CPUQuota="+strings.TrimSpace(v))
	}
	for _, p := range props {
		argv = append(argv, "-p", p)
	}
	argv = append(argv, "--", "sh", "-c", inner)

	s, err := c.NewSession()
	if err != nil {
		end("failed", "", err.Error())
		return
	}
	defer s.Close()
	stdout, err := s.StdoutPipe()
	if err != nil {
		end("failed", "", err.Error())
		return
	}
	var stderr strings.Builder
	s.Stderr = &stderr
	s.Stdin = strings.NewReader(prompt)
	started := time.Now()
	if err := s.Start(remote.Quote(argv)); err != nil {
		end("failed", "", err.Error())
		return
	}

	var report string
	lines, flushed := j.batchLines(ctx, job.ID)
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines <- line
		var ev struct {
			Type    string `json:"type"`
			ID      string `json:"id"`
			Model   string `json:"model"`
			Message *struct {
				Role    string `json:"role"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
			Usage *struct {
				Input      int64 `json:"input"`
				Output     int64 `json:"output"`
				CacheRead  int64 `json:"cacheRead"`
				CacheWrite int64 `json:"cacheWrite"`
				Cost       struct {
					Total float64 `json:"total"`
				} `json:"cost"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		switch {
		case ev.Type == "session" && ev.ID != "":
			_ = j.Store.SetJobSession(ctx, job.ID, ev.ID)
		case ev.Type == "message_end" && ev.Message != nil && ev.Message.Role == "assistant":
			var parts []string
			for _, c := range ev.Message.Content {
				if c.Type == "text" && strings.TrimSpace(c.Text) != "" {
					parts = append(parts, c.Text)
				}
			}
			if len(parts) > 0 {
				report = strings.Join(parts, "\n")
			}
			if ev.Usage != nil {
				_ = j.Store.RecordUsage(ctx, h.ID, job.ID, store.Usage{
					Model: ev.Model, Input: ev.Usage.Input, Output: ev.Usage.Output,
					CacheRead: ev.Usage.CacheRead, CacheWrite: ev.Usage.CacheWrite, Cost: ev.Usage.Cost.Total,
				})
			}
		}
	}
	close(lines)
	<-flushed
	err = s.Wait()
	// Whatever omp said on stderr is part of the record too.
	if se := strings.TrimSpace(stderr.String()); se != "" {
		b, _ := json.Marshal(map[string]string{"type": "stderr", "text": se})
		_ = j.Store.AppendJobEvent(ctx, job.ID, string(b))
	}
	var ee *ssh.ExitError
	switch {
	case err == nil:
		end("done", report, "")
	case errors.As(err, &ee):
		reason := fmt.Sprintf("omp exited %d", ee.ExitStatus())
		if time.Since(started) >= time.Duration(job.TimeoutS)*time.Second {
			reason = fmt.Sprintf("ran out of the %d s allowed", job.TimeoutS)
		}
		if t := lastLine(stderr.String()); t != "" {
			reason += ": " + t
		}
		if ee.Signal() == "KILL" {
			reason = "omp was killed — on a small machine this is the kernel's OOM killer; the agent needs about 1.5 GB"
		}
		end("failed", report, reason)
	default:
		end("failed", report, err.Error())
	}
}

// snapshot runs snapshot.sh on the connection as root and returns what
// it printed: the kind taken, or the outcome of a rollback.
func (j *Jobs) snapshot(ctx context.Context, c *ssh.Client, op string, id int64) (string, error) {
	r, err := remote.RunOn(ctx, c, []string{"sudo", "-n", "sh", "-s", "--", op, strconv.FormatInt(id, 10)}, strings.NewReader(snapshotScript))
	if err != nil {
		return "", err
	}
	if r.ExitCode != 0 {
		return "", errors.New(lastLine(string(r.Stderr)))
	}
	return strings.TrimSpace(string(r.Stdout)), nil
}

// Rollback puts the host back to the snapshot taken before a finished
// job: live on btrfs; on LVM the merge lands at the next boot.
func (j *Jobs) Rollback(ctx context.Context, id int64) (*store.Job, error) {
	job, err := j.Store.Job(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.State == "running" {
		return nil, errors.New("the job is still running")
	}
	if job.Snapshot != "btrfs" && job.Snapshot != "lvm" {
		return nil, fmt.Errorf("job %d has no snapshot to roll back to (%s); the rebuild script is the way back", id, orNone(job.Snapshot))
	}
	h, err := j.Store.Host(ctx, strconv.FormatInt(job.HostID, 10))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	c, err := j.Fleet.Exec.Dial(ctx, fleet.Target(h))
	if err != nil {
		return nil, err
	}
	defer c.Close()
	out, err := j.snapshot(ctx, c, "rollback", id)
	if err != nil {
		j.Notify("job.rollback_failed", h.Name, fmt.Sprintf("job %d on %s: rollback failed: %s", id, h.Name, err))
		return nil, err
	}
	_ = j.Store.SetJobSnapshot(ctx, id, out)
	j.Notify("job.rolled_back", h.Name, fmt.Sprintf("job %d on %s: %s", id, h.Name, map[string]string{
		"restored": "restored to the snapshot taken before it", "merge-at-boot": "the snapshot merges at the next boot; reboot when ready",
	}[out]))
	return j.Store.Job(ctx, id)
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func settingInt(ctx context.Context, st *store.Store, key string, def int) int {
	v, _ := st.Setting(ctx, key)
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
		return n
	}
	return def
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	if len(s) > 300 {
		s = s[len(s)-300:]
	}
	return s
}

// batchLines takes a job's output one line at a time and writes it in
// batches — every quarter second or fifty lines, one transaction — so a
// chatty omp is not one commit per line. Close the channel, then wait
// on flushed, and every line is in the store.
func (j *Jobs) batchLines(ctx context.Context, jobID int64) (chan<- string, <-chan struct{}) {
	lines := make(chan string, 256)
	flushed := make(chan struct{})
	go func() {
		defer close(flushed)
		var buf []string
		flush := func() {
			if len(buf) == 0 {
				return
			}
			// context.Background: a cancelled job still keeps what it said.
			if err := j.Store.AppendJobEvents(context.Background(), jobID, buf); err != nil {
				j.Agent.log.Error("append job events", "job", jobID, "err", err)
			}
			buf = buf[:0]
		}
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					flush()
					return
				}
				buf = append(buf, l)
				if len(buf) >= 50 {
					flush()
				}
			case <-tick.C:
				flush()
			}
		}
	}()
	return lines, flushed
}
