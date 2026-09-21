// Package tasks is the schedule: a command, a host (or every host), and
// when. The hub holds all three and runs the command down the SSH path
// enrollment opened, so there is no crontab anywhere to fall out of step
// with the panel. Every fire is recorded; runs never stack; an offline
// host is skipped and recorded; a hub that was down over a tick makes
// nothing up.
package tasks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/adefesioso/homedash/hub/internal/fleet"
	"github.com/adefesioso/homedash/hub/internal/gate"
	"github.com/adefesioso/homedash/hub/internal/store"
)

// Scheduler runs the tasks table.
type Scheduler struct {
	Store  *store.Store
	Fleet  *fleet.Fleet
	Notify fleet.Notify
	Log    *slog.Logger

	cron    *cron.Cron
	mu      sync.Mutex
	entries map[int64]cron.EntryID
	running map[int64]bool
}

// New builds a scheduler; Start loads the table and begins ticking.
func New(st *store.Store, fl *fleet.Fleet, notify fleet.Notify, log *slog.Logger) *Scheduler {
	return &Scheduler{Store: st, Fleet: fl, Notify: notify, Log: log,
		cron: cron.New(cron.WithParser(parser)), entries: map[int64]cron.EntryID{}, running: map[int64]bool{}}
}

// parser accepts five-field cron lines and the @hourly/@daily words.
var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// Validate is what Save checks: the schedule parses and the command is
// not something the gate refuses.
func Validate(t *store.Task) error {
	if strings.TrimSpace(t.Name) == "" {
		return errors.New("a task needs a name")
	}
	if strings.TrimSpace(t.Command) == "" {
		return errors.New("a task needs a command")
	}
	if _, err := parser.Parse(t.Schedule); err != nil {
		return fmt.Errorf("schedule: %w", err)
	}
	if t.TimeoutS <= 0 {
		t.TimeoutS = 600
	} else if t.TimeoutS > 86400 {
		return errors.New("timeout is at most 86400 seconds (a day)")
	}
	return gate.Check(t.Command)
}

// Start loads every task and keeps the clock running until ctx ends.
func (s *Scheduler) Start(ctx context.Context) error {
	ts, err := s.Store.Tasks(ctx)
	if err != nil {
		return err
	}
	for i := range ts {
		s.schedule(&ts[i])
	}
	s.cron.Start()
	go func() {
		<-ctx.Done()
		s.cron.Stop()
	}()
	return nil
}

// Save validates, stores and (re)schedules a task.
func (s *Scheduler) Save(ctx context.Context, t *store.Task) (*store.Task, error) {
	if err := Validate(t); err != nil {
		return nil, err
	}
	if t.HostID != 0 {
		if _, err := s.Store.Host(ctx, fmt.Sprint(t.HostID)); err != nil {
			return nil, err
		}
	}
	id, err := s.Store.SaveTask(ctx, t)
	if err != nil {
		return nil, err
	}
	saved, err := s.Store.Task(ctx, id)
	if err != nil {
		return nil, err
	}
	s.schedule(saved)
	return saved, nil
}

// Delete removes a task from the clock and the table.
func (s *Scheduler) Delete(ctx context.Context, id int64) error {
	s.mu.Lock()
	if e, ok := s.entries[id]; ok {
		s.cron.Remove(e)
		delete(s.entries, id)
	}
	s.mu.Unlock()
	return s.Store.DeleteTask(ctx, id)
}

func (s *Scheduler) schedule(t *store.Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[t.ID]; ok {
		s.cron.Remove(e)
		delete(s.entries, t.ID)
	}
	if !t.Enabled {
		return
	}
	id := t.ID
	e, err := s.cron.AddFunc(t.Schedule, func() { s.Fire(context.Background(), id, false) })
	if err != nil {
		s.Log.Error("schedule task", "task", t.Name, "err", err)
		return
	}
	s.entries[t.ID] = e
}

// Fire runs a task now: on its host, or on every enrolled remote one at
// a time. A task already running is not started again. The gate is
// checked on every fire, so a command that became refusable stops.
func (s *Scheduler) Fire(ctx context.Context, id int64, byHand bool) {
	s.mu.Lock()
	if s.running[id] {
		s.mu.Unlock()
		return
	}
	s.running[id] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.running, id)
		s.mu.Unlock()
	}()

	t, err := s.Store.Task(ctx, id)
	if err != nil {
		return
	}
	if err := gate.Check(t.Command); err != nil {
		_, _ = s.Store.StartRun(ctx, t.ID, 0, "", err.Error())
		s.Notify("gate.refused", t.Name, "task "+t.Name+": "+err.Error())
		return
	}
	var hosts []store.Host
	if t.HostID != 0 {
		h, err := s.Store.Host(ctx, fmt.Sprint(t.HostID))
		if err != nil {
			return
		}
		hosts = []store.Host{*h}
	} else if hosts, err = s.Store.Hosts(ctx); err != nil {
		return
	}
	failed := false
	for i := range hosts {
		h := &hosts[i]
		if h.Status != "online" {
			_, _ = s.Store.StartRun(ctx, t.ID, h.ID, h.Name, "host is "+h.Status)
			continue
		}
		runID, err := s.Store.StartRun(ctx, t.ID, h.ID, h.Name, "")
		if err != nil {
			continue
		}
		r, err := s.Fleet.Run(ctx, h, t.Command, time.Duration(t.TimeoutS)*time.Second, false)
		switch {
		case err != nil:
			_ = s.Store.EndRun(ctx, runID, -1, err.Error())
			failed = true
		default:
			_ = s.Store.EndRun(ctx, runID, r.ExitCode, string(r.Stdout)+string(r.Stderr))
			if r.ExitCode != 0 {
				failed = true
			}
		}
	}
	_ = s.Store.TrimRuns(ctx, t.ID, 50)
	if changed, _ := s.Store.SetTaskFailing(ctx, t.ID, failed); changed {
		if failed {
			s.Notify("task.failing", t.Name, "task "+t.Name+" has started failing")
		} else {
			s.Notify("task.ok", t.Name, "task "+t.Name+" succeeded again")
		}
	}
}
