package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// Job is one remote's agent working on its own machine on the hub's
// instructions: what was asked, where, and how it ended.
type Job struct {
	ID       int64  `json:"id"`
	HostID   int64  `json:"hostId"`
	Host     string `json:"host"`
	Cwd      string `json:"cwd"`
	WindowID int64  `json:"windowId,omitempty"`
	Model    string `json:"model"`
	Text     string `json:"text"`
	Rounds   int    `json:"rounds"`
	State    string `json:"state"`
	TimeoutS int    `json:"timeoutSeconds"`
	Session  string `json:"session,omitempty"`
	Snapshot string `json:"snapshot,omitempty"`
	Report   string `json:"report,omitempty"`
	// Changes is the report's homedash-changes block, parsed: what the
	// rebuild script and the catalog are updated from. Absent when the
	// remote gave none.
	Changes json.RawMessage `json:"changes,omitempty"`
	Reason  string          `json:"reason,omitempty"`
	Started string          `json:"started"`
	Ended   string          `json:"ended,omitempty"`
}

const jobCols = `j.id, j.host_id, h.name, j.cwd, COALESCE(j.window_id, 0), j.model, j.text, j.rounds, j.state, j.timeout_s, j.session, j.snapshot, j.report, j.changes, j.reason, j.started, COALESCE(j.ended, '')`

func scanJob(sc interface{ Scan(...any) error }) (*Job, error) {
	var j Job
	var changes string
	if err := sc.Scan(&j.ID, &j.HostID, &j.Host, &j.Cwd, &j.WindowID, &j.Model, &j.Text, &j.Rounds, &j.State, &j.TimeoutS, &j.Session, &j.Snapshot, &j.Report, &changes, &j.Reason, &j.Started, &j.Ended); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("no such job")
		}
		return nil, err
	}
	if changes != "" {
		j.Changes = json.RawMessage(changes)
	}
	return &j, nil
}

// CreateJob records a job about to start.
func (s *Store) CreateJob(ctx context.Context, hostID, windowID int64, cwd, model, text string, timeoutS int) (int64, error) {
	var win any
	if windowID != 0 {
		win = windowID
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO jobs(host_id, cwd, window_id, model, text, state, timeout_s) VALUES (?, ?, ?, ?, ?, 'running', ?)`,
		hostID, cwd, win, model, text, timeoutS)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Job reads one.
func (s *Store) Job(ctx context.Context, id int64) (*Job, error) {
	return scanJob(s.RO.QueryRowContext(ctx, `SELECT `+jobCols+` FROM jobs j JOIN hosts h ON h.id = j.host_id WHERE j.id = ?`, id))
}

// Jobs lists, newest first, optionally for one host.
func (s *Store) Jobs(ctx context.Context, hostID int64, limit int) ([]Job, error) {
	q := `SELECT ` + jobCols + ` FROM jobs j JOIN hosts h ON h.id = j.host_id`
	args := []any{}
	if hostID != 0 {
		q += ` WHERE j.host_id = ?`
		args = append(args, hostID)
	}
	q += ` ORDER BY j.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.RO.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

// SetJobSession records the remote's own session id, for corrections.
func (s *Store) SetJobSession(ctx context.Context, id int64, session string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE jobs SET session = ? WHERE id = ? AND session = ''`, session, id)
	return err
}

// SetJobSnapshot records what the hub could snapshot before a round:
// btrfs, lvm, none, or restored after a rollback.
func (s *Store) SetJobSnapshot(ctx context.Context, id int64, kind string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE jobs SET snapshot = ? WHERE id = ?`, kind, id)
	return err
}

// SetJobChanges keeps the change report a round ended with; "" clears it.
func (s *Store) SetJobChanges(ctx context.Context, id int64, changes string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE jobs SET changes = ? WHERE id = ?`, changes, id)
	return err
}

// StartRound marks a correction: another round, running again.
func (s *Store) StartRound(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE jobs SET rounds = rounds + 1, state = 'running', ended = NULL, reason = '' WHERE id = ?`, id)
	return err
}

// EndJob records how a round ended. Guarded to running rows only: a
// round that was killed already moved the state past running, and this
// call — arriving after, once the round notices its session died —
// must not overwrite that with its own idea of how the job ended.
func (s *Store) EndJob(ctx context.Context, id int64, state, report, reason string) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE jobs SET state = ?, report = ?, reason = ?, ended = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ? AND state = 'running'`,
		state, report, reason, id)
	return err
}

// KillJob marks a running job killed, same guard as EndJob so a
// concurrent normal end can't race it either way. Reports whether this
// call was the one that took effect.
func (s *Store) KillJob(ctx context.Context, id int64, reason string) (bool, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE jobs SET state = 'killed', reason = ?, ended = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ? AND state = 'running'`,
		reason, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// AppendJobEvent keeps one line the remote's omp emitted.
func (s *Store) AppendJobEvent(ctx context.Context, jobID int64, line string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO job_events(job_id, line) VALUES (?, ?)`, jobID, line)
	return err
}

// AppendJobEvents keeps several lines in one transaction: what the job
// runner hands over every quarter second, so a chatty job is not one
// commit per line.
func (s *Store) AppendJobEvents(ctx context.Context, jobID int64, lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	st, err := tx.PrepareContext(ctx, `INSERT INTO job_events(job_id, line) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer st.Close()
	for _, l := range lines {
		if _, err := st.ExecContext(ctx, jobID, l); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// JobEvent is one line of a job's stream.
type JobEvent struct {
	ID   int64  `json:"id"`
	At   string `json:"at"`
	Line string `json:"line"`
}

// JobEvents returns a job's lines after the given id, in order.
func (s *Store) JobEvents(ctx context.Context, jobID, after int64, limit int) ([]JobEvent, error) {
	rows, err := s.RO.QueryContext(ctx,
		`SELECT id, at, line FROM job_events WHERE job_id = ? AND id > ? ORDER BY id LIMIT ?`, jobID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JobEvent{}
	for rows.Next() {
		var e JobEvent
		if err := rows.Scan(&e.ID, &e.At, &e.Line); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// TrimJobs applies retention per host: the newest keep jobs stay, and
// the rest go with their events. Running jobs are never trimmed.
// It returns the ids that went, so their snapshots can go too.
func (s *Store) TrimJobs(ctx context.Context, hostID int64, keep int) ([]int64, error) {
	if keep < 5 {
		keep = 5
	}
	rows, err := s.DB.QueryContext(ctx,
		`DELETE FROM jobs WHERE host_id = ? AND state != 'running' AND id NOT IN (
			SELECT id FROM jobs WHERE host_id = ? ORDER BY id DESC LIMIT ?) RETURNING id`, hostID, hostID, keep)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ClearJobs is Clear history: every job not running goes, with its
// events, on every host. It returns the ids that went, grouped by host,
// so their snapshots can go too.
func (s *Store) ClearJobs(ctx context.Context) (map[int64][]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `DELETE FROM jobs WHERE state != 'running' RETURNING host_id, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	gone := map[int64][]int64{}
	for rows.Next() {
		var hostID, id int64
		if err := rows.Scan(&hostID, &id); err != nil {
			return nil, err
		}
		gone[hostID] = append(gone[hostID], id)
	}
	return gone, rows.Err()
}
