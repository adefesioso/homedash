package store

import (
	"context"
	"database/sql"
	"errors"
)

const taskSchema = `
CREATE TABLE IF NOT EXISTS tasks (
	id        INTEGER PRIMARY KEY,
	name      TEXT NOT NULL,
	host_id   INTEGER REFERENCES hosts(id) ON DELETE CASCADE,
	command   TEXT NOT NULL,
	schedule  TEXT NOT NULL,
	timeout_s INTEGER NOT NULL DEFAULT 600,
	enabled   INTEGER NOT NULL DEFAULT 1,
	failing   INTEGER NOT NULL DEFAULT 0,
	created   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE TABLE IF NOT EXISTS task_runs (
	id        INTEGER PRIMARY KEY,
	task_id   INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	host_id   INTEGER REFERENCES hosts(id) ON DELETE SET NULL,
	host      TEXT NOT NULL,
	started   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	ended     TEXT,
	exit_code INTEGER,
	output    TEXT NOT NULL DEFAULT '',
	skipped   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS task_runs_task ON task_runs(task_id, id DESC);
`

// Task is a command, a host (or every host), and a schedule.
type Task struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	HostID   int64  `json:"hostId"` // 0: every enrolled remote
	Host     string `json:"host,omitempty"`
	Command  string `json:"command"`
	Schedule string `json:"schedule"`
	TimeoutS int    `json:"timeoutSeconds"`
	Enabled  bool   `json:"enabled"`
	Failing  bool   `json:"failing"`
	Created  string `json:"created"`
}

const taskCols = `t.id, t.name, COALESCE(t.host_id, 0), COALESCE(h.name, ''), t.command, t.schedule, t.timeout_s, t.enabled, t.failing, t.created`

func scanTask(sc interface{ Scan(...any) error }) (*Task, error) {
	var t Task
	if err := sc.Scan(&t.ID, &t.Name, &t.HostID, &t.Host, &t.Command, &t.Schedule, &t.TimeoutS, &t.Enabled, &t.Failing, &t.Created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("no such task")
		}
		return nil, err
	}
	return &t, nil
}

// Tasks lists every task in the house.
func (s *Store) Tasks(ctx context.Context) ([]Task, error) {
	rows, err := s.RO.QueryContext(ctx, `SELECT `+taskCols+` FROM tasks t LEFT JOIN hosts h ON h.id = t.host_id ORDER BY t.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// Task reads one.
func (s *Store) Task(ctx context.Context, id int64) (*Task, error) {
	return scanTask(s.RO.QueryRowContext(ctx, `SELECT `+taskCols+` FROM tasks t LEFT JOIN hosts h ON h.id = t.host_id WHERE t.id = ?`, id))
}

// SaveTask inserts (id 0) or updates.
func (s *Store) SaveTask(ctx context.Context, t *Task) (int64, error) {
	var host any
	if t.HostID != 0 {
		host = t.HostID
	}
	if t.ID == 0 {
		res, err := s.DB.ExecContext(ctx,
			`INSERT INTO tasks(name, host_id, command, schedule, timeout_s, enabled) VALUES (?, ?, ?, ?, ?, ?)`,
			t.Name, host, t.Command, t.Schedule, t.TimeoutS, t.Enabled)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	_, err := s.DB.ExecContext(ctx,
		`UPDATE tasks SET name = ?, host_id = ?, command = ?, schedule = ?, timeout_s = ?, enabled = ? WHERE id = ?`,
		t.Name, host, t.Command, t.Schedule, t.TimeoutS, t.Enabled, t.ID)
	return t.ID, err
}

// DeleteTask removes a task and its runs.
func (s *Store) DeleteTask(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	return err
}

// SetTaskFailing records the transition the notification is about and
// returns whether it changed.
func (s *Store) SetTaskFailing(ctx context.Context, id int64, failing bool) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE tasks SET failing = ? WHERE id = ? AND failing != ?`, failing, id, failing)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// TaskRun is one fire on one host.
type TaskRun struct {
	ID       int64  `json:"id"`
	TaskID   int64  `json:"taskId"`
	Host     string `json:"host"`
	Started  string `json:"started"`
	Ended    string `json:"ended,omitempty"`
	ExitCode *int   `json:"exitCode"`
	Output   string `json:"output"`
	Skipped  string `json:"skipped,omitempty"`
}

// StartRun records a fire beginning (or a skip, when skipped is set).
func (s *Store) StartRun(ctx context.Context, taskID, hostID int64, host, skipped string) (int64, error) {
	var hid any
	if hostID != 0 {
		hid = hostID
	}
	var ended any
	if skipped != "" {
		ended = "now"
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO task_runs(task_id, host_id, host, skipped, ended) VALUES (?, ?, ?, ?, CASE WHEN ? IS NULL THEN NULL ELSE strftime('%Y-%m-%dT%H:%M:%fZ','now') END)`,
		taskID, hid, host, skipped, ended)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// EndRun records how a fire ended.
func (s *Store) EndRun(ctx context.Context, id int64, exitCode int, output string) error {
	if len(output) > 64<<10 {
		output = output[len(output)-64<<10:]
	}
	_, err := s.DB.ExecContext(ctx,
		`UPDATE task_runs SET ended = strftime('%Y-%m-%dT%H:%M:%fZ','now'), exit_code = ?, output = ? WHERE id = ?`, exitCode, output, id)
	return err
}

// TaskRuns lists the newest runs of a task.
func (s *Store) TaskRuns(ctx context.Context, taskID int64, limit int) ([]TaskRun, error) {
	rows, err := s.RO.QueryContext(ctx,
		`SELECT id, task_id, host, started, COALESCE(ended, ''), exit_code, output, skipped FROM task_runs WHERE task_id = ? ORDER BY id DESC LIMIT ?`, taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskRun{}
	for rows.Next() {
		var r TaskRun
		var code sql.NullInt64
		if err := rows.Scan(&r.ID, &r.TaskID, &r.Host, &r.Started, &r.Ended, &code, &r.Output, &r.Skipped); err != nil {
			return nil, err
		}
		if code.Valid {
			c := int(code.Int64)
			r.ExitCode = &c
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TrimRuns keeps the newest runs per task.
func (s *Store) TrimRuns(ctx context.Context, taskID int64, keep int) error {
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM task_runs WHERE task_id = ? AND id NOT IN (SELECT id FROM task_runs WHERE task_id = ? ORDER BY id DESC LIMIT ?)`, taskID, taskID, keep)
	return err
}

const catalogSchema = `
CREATE TABLE IF NOT EXISTS catalog (
	name  TEXT PRIMARY KEY,
	entry TEXT NOT NULL
);
`

// CatalogEntries returns the entries you added, as JSON.
func (s *Store) CatalogEntries(ctx context.Context) ([]string, error) {
	rows, err := s.RO.QueryContext(ctx, `SELECT entry FROM catalog ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetCatalogEntry stores or replaces one of your entries.
func (s *Store) SetCatalogEntry(ctx context.Context, name, entry string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO catalog(name, entry) VALUES (?, ?) ON CONFLICT(name) DO UPDATE SET entry = excluded.entry`, name, entry)
	return err
}

// DeleteCatalogEntry removes one of your entries.
func (s *Store) DeleteCatalogEntry(ctx context.Context, name string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM catalog WHERE name = ?`, name)
	return err
}
