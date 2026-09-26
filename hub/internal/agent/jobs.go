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
// the hub's agent reads, and the homedash-changes block in it is what the
// rebuild script and the catalog are folded from.
const reportInstruction = `You are root on this machine, working alone on behalf of the HomeDash hub. The machine is yours to change: packages, drivers, services, containers of any kind, files anywhere, a data disk to partition, format and mount. Three things you must never do: (1) cut this machine's connection to the hub — leave sshd and /etc/ssh, the homedash account, its key and its line in /etc/sudoers.d, and the network path to the hub alone, and do not reboot or shut down; (2) touch the hub; (3) touch another machine in the house — never ssh to, mount, or connect to an address in ` + fleet.FleetAddrs + `. If the work needs another machine, say so in your report: the hub gives that machine its own job, or shares a workspace between you. The hub checks its hold on this machine when you finish and puts back anything you changed. A refused tool call comes back as an error you must relay, not route around. Do the work below, then end with a section titled "Report": what you changed and what you could not do. Close the report with a fenced code block tagged homedash-changes holding one JSON object, every key optional: "packages", "services", "files", "mounts" (arrays of the shell commands that make each change again on a fresh Debian, in order; a small file inline as a heredoc), "stacks" (compose projects you started, stopped or changed, with their compose file's path), "catalog" (array of {"entry", "note"}: a catalog entry this machine proved wrong or out of date, or a stack worth saving), "data" (paths that must survive a rebuild), "proposal" ({"title", "body"}: only when something about HomeDash itself would have made this job easier).

`

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
		cwd = gate.AgentHome
	}
	if !strings.HasPrefix(cwd, "/") {
		return nil, errors.New("the working directory must be an absolute path")
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
	if r, err := remote.RunOn(ctx, c, []string{"sudo", "-n", "test", "-d", cwd}, nil); err != nil || r.ExitCode != 0 {
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
	// What the job meets on its own tools, fresh: whatever the last round
	// did to the hook or the fleet's addresses is undone here.
	if err := j.Fleet.ArmJob(ctx, c, h); err != nil {
		end("failed", "", "could not write the hook: "+err.Error())
		return
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
	// Root, in a unit: a name Kill can stop, a clock, a task cap, and the
	// memory and CPU caps Settings sets. Nothing else is fenced — the
	// machine is the job's; the hub's hold is restored after.
	argv := []string{"sudo", "-n", "systemd-run", "--quiet", "--pipe", "--wait", "--collect",
		"--unit=homedash-job-" + strconv.FormatInt(job.ID, 10) + "-r" + strconv.Itoa(job.Rounds),
		"--working-directory=" + job.Cwd,
		"--setenv=HOME=" + gate.AgentHome, "--setenv=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "--setenv=LANG=C.UTF-8",
		"--setenv=OMP_AUTH_BROKER_URL=http://" + j.Fleet.VaultAddr}
	// The hub's hold is put back when the unit stops, however it stops —
	// done, timed out, killed — as root, before the hub looks again over
	// its own sudo, which the job may have been what broke.
	props := []string{"TasksMax=4096", "RuntimeMaxSec=" + strconv.Itoa(job.TimeoutS+60), "ExecStopPost=" + j.Fleet.HoldStopPost()}
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
	j.checkHold(ctx, c, job, h)
	if changes, bad := parseChanges(report); bad != "" {
		b, _ := json.Marshal(map[string]string{"type": "stderr", "text": "change report: " + bad})
		_ = j.Store.AppendJobEvent(ctx, job.ID, string(b))
	} else if changes != "" {
		_ = j.Store.SetJobChanges(ctx, job.ID, changes)
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

// checkHold puts back whatever of the hub's hold the round changed, over
// the round's own connection, and says so in the job and the event log.
func (j *Jobs) checkHold(ctx context.Context, c *ssh.Client, job *store.Job, h *store.Host) {
	restored, broken, err := j.Fleet.CheckHold(ctx, c)
	line := func(text string) {
		b, _ := json.Marshal(map[string]string{"type": "hold", "text": text})
		_ = j.Store.AppendJobEvent(ctx, job.ID, string(b))
	}
	switch {
	case err != nil:
		line("could not check the hub's hold: " + err.Error())
		j.Notify("host.hold_unchecked", h.Name, fmt.Sprintf("job %d on %s: could not check the hub's hold: %s", job.ID, h.Name, err))
		return
	case restored != "":
		line("restored: " + restored)
		j.Notify("host.hold_repaired", h.Name, fmt.Sprintf("job %d on %s changed the hub's hold; restored: %s", job.ID, h.Name, restored))
	}
	if broken != "" {
		line("broken: " + broken)
		j.Notify("host.hold_broken", h.Name, fmt.Sprintf("job %d on %s: the hub's hold needs a person: %s", job.ID, h.Name, broken))
	}
}

// parseChanges finds the last homedash-changes fenced block in a report
// and returns it as compact JSON. No block is "", ""; a block that is not
// one JSON object is "", and why.
func parseChanges(report string) (string, string) {
	const fence = "```homedash-changes"
	i := strings.LastIndex(report, fence)
	if i < 0 {
		return "", ""
	}
	body := report[i+len(fence):]
	end := strings.Index(body, "```")
	if end < 0 {
		return "", "the homedash-changes block is not closed"
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(body[:end])), &obj); err != nil {
		return "", "the homedash-changes block is not a JSON object: " + err.Error()
	}
	out, _ := json.Marshal(obj)
	return string(out), ""
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

// Kill stops a running job on its remote. It marks the row killed
// first, guarded to running rows only, so the round it interrupts —
// mid systemd-run --wait, about to notice its session died and call
// EndJob itself — finds the row already past running and no-ops rather
// than overwriting killed with failed. The host has to be online: with
// no connection there is nothing to stop the process with.
func (j *Jobs) Kill(ctx context.Context, id int64) (*store.Job, error) {
	job, err := j.Store.Job(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.State != "running" {
		return nil, errors.New("the job is not running")
	}
	h, err := j.Store.Host(ctx, strconv.FormatInt(job.HostID, 10))
	if err != nil {
		return nil, err
	}
	if h.Status != "online" {
		return nil, fmt.Errorf("%s is offline; the job cannot be stopped until it reconnects", h.Name)
	}
	ok, err := j.Store.KillJob(ctx, id, "stopped by request")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("the job is not running")
	}
	c, err := j.Fleet.Exec.Dial(ctx, fleet.Target(h))
	if err == nil {
		defer c.Close()
		unit := "homedash-job-" + strconv.FormatInt(id, 10) + "-r" + strconv.Itoa(job.Rounds)
		_, _ = remote.RunOn(ctx, c, []string{"sudo", "-n", "systemctl", "stop", "--no-block", unit}, nil)
	}
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
