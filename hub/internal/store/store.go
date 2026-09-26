// Package store is the one database file on the hub. An absent file is
// created on first start; there is no migration tooling to run.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Store wraps the one file's two pools: DB, one connection, is the single
// writer; RO, a few read-only connections, is what every list and lookup
// reads through, so a read never queues behind a write (WAL makes that
// safe). Writes, and any read that must see a write in the same flow,
// go through DB or a transaction on it.
type Store struct {
	DB   *sql.DB
	RO   *sql.DB
	Path string
}

// Open opens (or creates) homedash.db under stateDir and applies the schema.
func Open(ctx context.Context, stateDir string) (*Store, error) {
	path := filepath.Join(stateDir, "homedash.db")
	// synchronous=NORMAL under WAL: a commit is durable against the hub
	// crashing, and only a power cut can lose the last moments — a metric
	// row or a job line — for a fsync per checkpoint instead of per commit.
	const pragmas = "_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", "file:"+path+"?"+pragmas)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// One file, one process: a single writer keeps SQLite honest.
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	for _, sch := range []string{hostSchema, schema, secretSchema, taskSchema, catalogSchema, clusterSchema, authSchema, peerSchema, deviceSchema} {
		if _, err := db.ExecContext(ctx, sch); err != nil {
			return nil, fmt.Errorf("apply schema: %w", err)
		}
	}
	// A hub upgraded from before tokens had unique names might still hold
	// duplicates; drop all but the newest per name before the index below
	// can be created (C-6).
	if err := dedupeTokenNames(ctx, db); err != nil {
		return nil, fmt.Errorf("dedupe tokens: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS tokens_name ON tokens(name)`); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	// Columns added after a table first shipped: idempotent, no tooling.
	for _, c := range []struct{ table, column, def string }{
		{"hosts", "vault_token", "TEXT NOT NULL DEFAULT ''"},
		{"hosts", "credentials_revoked_at", "TEXT NOT NULL DEFAULT ''"},
		{"hosts", "kind", "TEXT NOT NULL DEFAULT 'compute'"},
		{"enroll_codes", "kind", "TEXT NOT NULL DEFAULT 'compute'"},
		{"jobs", "snapshot", "TEXT NOT NULL DEFAULT ''"},
		{"jobs", "changes", "TEXT NOT NULL DEFAULT ''"},
		{"windows", "scrollback", "BLOB"},
	} {
		if err := addColumn(ctx, db, c.table, c.column, c.def); err != nil {
			return nil, err
		}
	}
	ro, err := sql.Open("sqlite", "file:"+path+"?mode=ro&"+pragmas)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	ro.SetMaxOpenConns(4)
	if err := ro.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s read-only: %w", path, err)
	}
	return &Store{DB: db, RO: ro, Path: path}, nil
}

// addColumn adds a column if the table lacks it.
func addColumn(ctx context.Context, db *sql.DB, table, column, def string) error {
	rows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, def))
	return err
}

// Close releases the database.
func (s *Store) Close() error {
	rerr := s.RO.Close()
	if err := s.DB.Close(); err != nil {
		return err
	}
	return rerr
}

// schema is applied on every start and is idempotent. Tables land here as
// the features that own them are built; the events table exists from the
// first start because the first start is itself an event.
const schema = `
CREATE TABLE IF NOT EXISTS events (
	id        INTEGER PRIMARY KEY,
	at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	kind      TEXT NOT NULL,
	subject   TEXT NOT NULL DEFAULT '',
	message   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS events_at ON events(at DESC);

CREATE TABLE IF NOT EXISTS settings (
	key       TEXT PRIMARY KEY,
	value     TEXT NOT NULL
);

-- A window is one omp session on the hub. Live means its process is up in
-- this hub process; ended is set when it exits or the hub stops.
CREATE TABLE IF NOT EXISTS windows (
	id        INTEGER PRIMARY KEY,
	name      TEXT NOT NULL,
	created   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	last_activity TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	ended     TEXT,
	scrollback BLOB
);

-- A job is one remote's agent working on its own machine on the hub's
-- instructions. The runner lands with the SSH executor; the tables are the
-- shape the window tools and the panel already read.
CREATE TABLE IF NOT EXISTS jobs (
	id        INTEGER PRIMARY KEY,
	host_id   INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
	cwd       TEXT NOT NULL,
	window_id INTEGER REFERENCES windows(id) ON DELETE SET NULL,
	model     TEXT NOT NULL DEFAULT '',
	text      TEXT NOT NULL,
	rounds    INTEGER NOT NULL DEFAULT 1,
	state     TEXT NOT NULL CHECK (state IN ('running','done','failed','needs_you')),
	timeout_s INTEGER NOT NULL,
	session   TEXT NOT NULL DEFAULT '',
	report    TEXT NOT NULL DEFAULT '',
	reason    TEXT NOT NULL DEFAULT '',
	started   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	ended     TEXT
);
CREATE TABLE IF NOT EXISTS job_events (
	id        INTEGER PRIMARY KEY,
	job_id    INTEGER NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
	at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	line      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS job_events_job ON job_events(job_id, id);

-- A proposal is an issue the hub's agent filed on the project's
-- repository; kept for the daily cap and the link back.
CREATE TABLE IF NOT EXISTS proposals (
	id        INTEGER PRIMARY KEY,
	title     TEXT NOT NULL,
	url       TEXT NOT NULL,
	created   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
`

// Event is one row of the event log: a transition, not a state.
type Event struct {
	ID      int64  `json:"id"`
	At      string `json:"at"`
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
	Message string `json:"message"`
}

// RecordEvent appends to the event log.
func (s *Store) RecordEvent(ctx context.Context, kind, subject, message string) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO events(kind, subject, message) VALUES (?, ?, ?)`, kind, subject, message)
	return err
}

// Events returns the newest events first, at most limit of them. before,
// if not zero, pages backward: only events with an id less than it, so
// the panel's "Older" button and the CLI's --before both walk the log
// without skipping or repeating a row as new ones land.
func (s *Store) Events(ctx context.Context, before int64, limit int) ([]Event, error) {
	var rows *sql.Rows
	var err error
	if before > 0 {
		rows, err = s.RO.QueryContext(ctx,
			`SELECT id, at, kind, subject, message FROM events WHERE id < ? ORDER BY at DESC, id DESC LIMIT ?`, before, limit)
	} else {
		rows, err = s.RO.QueryContext(ctx,
			`SELECT id, at, kind, subject, message FROM events ORDER BY at DESC, id DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.At, &e.Kind, &e.Subject, &e.Message); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SettingsWithPrefix reads every key under a prefix, as one query.
func (s *Store) SettingsWithPrefix(ctx context.Context, prefix string) (map[string]string, error) {
	rows, err := s.RO.QueryContext(ctx, `SELECT key, value FROM settings WHERE key >= ? AND key < ?`, prefix, prefix+"\xff")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// Setting reads one key; a missing key is the empty string.
func (s *Store) Setting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.RO.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// SetSetting writes one key.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// Window is one row of the windows table.
type Window struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Created      string `json:"created"`
	LastActivity string `json:"lastActivity"`
	Ended        string `json:"ended,omitempty"`
}

// CreateWindow records a new window and returns its id.
func (s *Store) CreateWindow(ctx context.Context, name string) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO windows(name) VALUES (?)`, name)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// TouchWindow moves a window's last activity to now.
func (s *Store) TouchWindow(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE windows SET last_activity = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`, id)
	return err
}

// EndWindow marks a window's process as gone and keeps the last of its
// output as the row's scrollback (nil when there is none). Idempotent.
func (s *Store) EndWindow(ctx context.Context, id int64, scrollback []byte) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE windows SET ended = strftime('%Y-%m-%dT%H:%M:%fZ','now'), scrollback = ? WHERE id = ? AND ended IS NULL`, scrollback, id)
	return err
}

// WindowScrollback is what a finished window last showed: the bytes
// EndWindow kept, nil for a live window or one a hub restart ended.
// sql.ErrNoRows when there is no such window.
func (s *Store) WindowScrollback(ctx context.Context, id int64) ([]byte, error) {
	var b []byte
	err := s.RO.QueryRowContext(ctx, `SELECT scrollback FROM windows WHERE id = ?`, id).Scan(&b)
	return b, err
}

// EndAllWindows is what a starting hub does: no process survived the
// restart, so every window still marked live is history. It returns the
// windows that were live, for the caller to record an event per window.
func (s *Store) EndAllWindows(ctx context.Context) ([]Window, error) {
	rows, err := s.RO.QueryContext(ctx,
		`SELECT id, name, created, last_activity, COALESCE(ended, '') FROM windows WHERE ended IS NULL`)
	if err != nil {
		return nil, err
	}
	live := []Window{}
	for rows.Next() {
		var w Window
		if err := rows.Scan(&w.ID, &w.Name, &w.Created, &w.LastActivity, &w.Ended); err != nil {
			rows.Close()
			return nil, err
		}
		live = append(live, w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE windows SET ended = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE ended IS NULL`); err != nil {
		return nil, err
	}
	return live, nil
}

// DeleteWindow removes a session from the list. Its jobs stay on the Jobs
// tab with their window_id cleared (ON DELETE SET NULL). sql.ErrNoRows
// when there is no such row.
func (s *Store) DeleteWindow(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM windows WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ClearWindows is Clear history on the Agents tab: every ended session
// goes; a live one is never touched. It returns how many went.
func (s *Store) ClearWindows(ctx context.Context) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM windows WHERE ended IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Windows lists every window, most recently active first.
func (s *Store) Windows(ctx context.Context) ([]Window, error) {
	rows, err := s.RO.QueryContext(ctx,
		`SELECT id, name, created, last_activity, COALESCE(ended, '') FROM windows ORDER BY last_activity DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Window{}
	for rows.Next() {
		var w Window
		if err := rows.Scan(&w.ID, &w.Name, &w.Created, &w.LastActivity, &w.Ended); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// Window reads one row.
func (s *Store) Window(ctx context.Context, id int64) (*Window, error) {
	var w Window
	err := s.RO.QueryRowContext(ctx,
		`SELECT id, name, created, last_activity, COALESCE(ended, '') FROM windows WHERE id = ?`, id).
		Scan(&w.ID, &w.Name, &w.Created, &w.LastActivity, &w.Ended)
	if err != nil {
		return nil, err
	}
	return &w, nil
}
