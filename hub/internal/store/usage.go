package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// usageSchema is the spend that isn't a remote job's: the hub agent's own
// replies, read from omp's session files, and the tokens the router's
// machines served, whoever asked. A remote job's replies stay in `usage`
// (hosts.go), which carries a host and a job these rows don't have.
const usageSchema = `
CREATE TABLE IF NOT EXISTS hub_usage (
	at          TEXT NOT NULL,
	model       TEXT NOT NULL,
	input       INTEGER NOT NULL,
	output      INTEGER NOT NULL,
	cache_read  INTEGER NOT NULL,
	cache_write INTEGER NOT NULL,
	cost        REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS hub_usage_at ON hub_usage(at);
CREATE TABLE IF NOT EXISTS hub_usage_files (
	path   TEXT PRIMARY KEY,
	pos    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS served (
	at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	server TEXT NOT NULL,
	peer   INTEGER NOT NULL,
	caller TEXT NOT NULL,
	model  TEXT NOT NULL,
	input  INTEGER NOT NULL,
	output INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS served_at ON served(at);
`

// usageKeep is how long hub and served rows are kept: the Usage tab's
// widest window.
const usageKeep = "-31 days"

// HubUsageOffset is how far into one session file the hub has already
// counted; 0 for a file it has never read.
func (s *Store) HubUsageOffset(ctx context.Context, path string) (int64, error) {
	var off int64
	err := s.RO.QueryRowContext(ctx, `SELECT pos FROM hub_usage_files WHERE path = ?`, path).Scan(&off)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return off, err
}

// RecordHubUsage keeps the replies read from one session file and moves
// its offset past them in the same commit, so a reply is never counted
// twice or skipped by a crash between the two.
func (s *Store) RecordHubUsage(ctx context.Context, path string, offset int64, us []Usage) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, u := range us {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO hub_usage(at, model, input, output, cache_read, cache_write, cost) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			u.At, u.Model, u.Input, u.Output, u.CacheRead, u.CacheWrite, u.Cost); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO hub_usage_files(path, pos) VALUES (?, ?)`, path, offset); err != nil {
		return err
	}
	return tx.Commit()
}

// HubUsage returns the hub agent's replies over the window, summed per
// model per hour, oldest first.
func (s *Store) HubUsage(ctx context.Context, hours int) ([]Usage, error) {
	rows, err := s.RO.QueryContext(ctx,
		`SELECT strftime('%Y-%m-%dT%H:00:00Z', at), model, SUM(input), SUM(output), SUM(cache_read), SUM(cache_write), SUM(cost), COUNT(*)
		 FROM hub_usage WHERE at >= ? GROUP BY 1, 2 ORDER BY 1`, since(hours))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Usage{}
	for rows.Next() {
		var u Usage
		if err := rows.Scan(&u.At, &u.Model, &u.Input, &u.Output, &u.CacheRead, &u.CacheWrite, &u.Cost, &u.Calls); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Served is tokens one machine answered through the router: a local
// Ollama host, or a peer (Peer) this hub placed a job on. Caller is who
// asked — `hub` (the hub agent), `remote:<host>` (a job through the job
// door), `peer:<name>` (a job from the space) or `client:<name>` (any
// other signed-in caller of the hub's Ollama endpoint).
type Served struct {
	At     string `json:"at"`
	Server string `json:"server"`
	Peer   bool   `json:"peer"`
	Caller string `json:"caller"`
	Model  string `json:"model"`
	Input  int64  `json:"input"`
	Output int64  `json:"output"`
	Calls  int64  `json:"calls"`
}

// RecordServed keeps one answered request.
func (s *Store) RecordServed(ctx context.Context, v Served) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO served(server, peer, caller, model, input, output) VALUES (?, ?, ?, ?, ?, ?)`,
		v.Server, v.Peer, v.Caller, v.Model, v.Input, v.Output)
	return err
}

// ServedUsage returns what was served over the window, summed per
// server, caller and model per hour, oldest first.
func (s *Store) ServedUsage(ctx context.Context, hours int) ([]Served, error) {
	rows, err := s.RO.QueryContext(ctx,
		`SELECT strftime('%Y-%m-%dT%H:00:00Z', at), server, peer, caller, model, SUM(input), SUM(output), COUNT(*)
		 FROM served WHERE at >= ? GROUP BY 1, 2, 3, 4, 5 ORDER BY 1`, since(hours))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Served{}
	for rows.Next() {
		var v Served
		if err := rows.Scan(&v.At, &v.Server, &v.Peer, &v.Caller, &v.Model, &v.Input, &v.Output, &v.Calls); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// pruneUsage drops hub and served rows past the widest window; called
// from RollupUsage, on the same hourly beat.
func (s *Store) pruneUsage(ctx context.Context) error {
	for _, q := range []string{
		`DELETE FROM hub_usage WHERE at < strftime('%Y-%m-%dT%H:%M:%fZ','now','` + usageKeep + `')`,
		`DELETE FROM served WHERE at < strftime('%Y-%m-%dT%H:%M:%fZ','now','` + usageKeep + `')`,
	} {
		if _, err := s.DB.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

func since(hours int) string {
	return time.Now().Add(-time.Duration(hours) * time.Hour).UTC().Format(time.RFC3339)
}
