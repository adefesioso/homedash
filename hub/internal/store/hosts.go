package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const hostSchema = `
CREATE TABLE IF NOT EXISTS hosts (
	id          INTEGER PRIMARY KEY,
	name        TEXT NOT NULL UNIQUE,
	addr        TEXT NOT NULL,
	port        INTEGER NOT NULL DEFAULT 22,
	user        TEXT NOT NULL DEFAULT 'homedash',
	host_key    TEXT NOT NULL,
	status      TEXT NOT NULL DEFAULT 'unknown' CHECK (status IN ('unknown','online','offline','mismatch')),
	facts       TEXT NOT NULL DEFAULT '{}',
	agent_model TEXT NOT NULL DEFAULT '',
	rebuild_script TEXT NOT NULL DEFAULT '',
	enrolled    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	last_seen   TEXT
);
CREATE TABLE IF NOT EXISTS enroll_codes (
	code         TEXT PRIMARY KEY,
	name         TEXT NOT NULL,
	rebuild_from INTEGER REFERENCES hosts(id) ON DELETE SET NULL,
	expires      TEXT NOT NULL,
	used         INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS metrics (
	host_id   INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
	at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	mem_used  INTEGER NOT NULL,
	mem_total INTEGER NOT NULL,
	load1     REAL NOT NULL,
	gpu_busy  INTEGER NOT NULL DEFAULT 0,
	mounts    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS metrics_host_at ON metrics(host_id, at);
CREATE TABLE IF NOT EXISTS metrics_hourly (
	host_id   INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
	hour      TEXT NOT NULL,
	mem_used  INTEGER NOT NULL,
	mem_total INTEGER NOT NULL,
	load1     REAL NOT NULL,
	gpu_busy  REAL NOT NULL,
	mounts    TEXT NOT NULL,
	PRIMARY KEY (host_id, hour)
);
CREATE TABLE IF NOT EXISTS usage (
	host_id     INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
	job_id      INTEGER NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
	at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	model       TEXT NOT NULL,
	input       INTEGER NOT NULL,
	output      INTEGER NOT NULL,
	cache_read  INTEGER NOT NULL,
	cache_write INTEGER NOT NULL,
	cost        REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS usage_host_at ON usage(host_id, at);
CREATE TABLE IF NOT EXISTS usage_hourly (
	host_id     INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
	hour        TEXT NOT NULL,
	input       INTEGER NOT NULL,
	output      INTEGER NOT NULL,
	cache_read  INTEGER NOT NULL,
	cache_write INTEGER NOT NULL,
	cost        REAL NOT NULL,
	calls       INTEGER NOT NULL,
	PRIMARY KEY (host_id, hour)
);
`

// ErrNoHost is an operation naming a host the hub does not have. The hub
// itself is never a host record, so there is no argument that means
// "here": that is the structural half of the gate.
var ErrNoHost = errors.New("no such host")

// Host is one enrolled machine as the store holds it.
type Host struct {
	ID            int64           `json:"id"`
	Name          string          `json:"name"`
	Addr          string          `json:"addr"`
	Port          int             `json:"port"`
	User          string          `json:"user"`
	HostKey       string          `json:"hostKey"`
	Status        string          `json:"status"`
	Facts         json.RawMessage `json:"facts"`
	AgentModel    string          `json:"agentModel"`
	RebuildScript string          `json:"rebuildScript"`
	VaultToken    string          `json:"-"`
	Enrolled      string          `json:"enrolled"`
	LastSeen      string          `json:"lastSeen,omitempty"`
	// CredentialsRevokedAt is set by Revoke and cleared by the next
	// Update credentials (H-9): the card reads "credentials revoked"
	// instead of an age while it holds a value.
	CredentialsRevokedAt string `json:"credentialsRevokedAt,omitempty"`
}

const hostCols = `id, name, addr, port, user, host_key, status, facts, agent_model, rebuild_script, vault_token, enrolled, COALESCE(last_seen, ''), credentials_revoked_at`

func scanHost(sc interface{ Scan(...any) error }) (*Host, error) {
	var h Host
	var facts string
	if err := sc.Scan(&h.ID, &h.Name, &h.Addr, &h.Port, &h.User, &h.HostKey, &h.Status, &facts, &h.AgentModel, &h.RebuildScript, &h.VaultToken, &h.Enrolled, &h.LastSeen, &h.CredentialsRevokedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoHost
		}
		return nil, err
	}
	h.Facts = json.RawMessage(facts)
	h.RebuildScript = unwrapRebuildScript(h.ID, h.RebuildScript)
	return &h, nil
}

// repairedRebuildScripts tracks which hosts have already been logged for
// unwrapRebuildScript, so a hub that reads a bitten host often (every
// sweep, every card refresh) doesn't say so more than once.
var repairedRebuildScripts sync.Map // host id -> struct{}

// unwrapRebuildScript is the one-off repair for hosts saved through the
// H-10 bug: the panel used to JSON-encode the script before PUTting it,
// so the store holds a quoted, escaped string instead of the script
// itself. Every reader goes through scanHost, so fixing it here fixes it
// everywhere, without touching what's on disk.
func unwrapRebuildScript(id int64, script string) string {
	if !strings.HasPrefix(script, `"`) {
		return script
	}
	var unwrapped string
	if err := json.Unmarshal([]byte(script), &unwrapped); err != nil {
		return script
	}
	if _, seen := repairedRebuildScripts.LoadOrStore(id, struct{}{}); !seen {
		slog.Default().Warn("rebuild script was stored JSON-encoded; unwrapped on read", "host", id)
	}
	return unwrapped
}

// Hosts lists every enrolled machine by name.
func (s *Store) Hosts(ctx context.Context) ([]Host, error) {
	rows, err := s.RO.QueryContext(ctx, `SELECT `+hostCols+` FROM hosts ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Host{}
	for rows.Next() {
		h, err := scanHost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *h)
	}
	return out, rows.Err()
}

// Host resolves a host by name or numeric id. Every remote operation
// starts here, and an unknown name is ErrNoHost.
func (s *Store) Host(ctx context.Context, ref string) (*Host, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, ErrNoHost
	}
	return scanHost(s.RO.QueryRowContext(ctx,
		`SELECT `+hostCols+` FROM hosts WHERE name = ? OR (CAST(id AS TEXT) = ? AND ? GLOB '[0-9]*')`, ref, ref, ref))
}

// AddHost is enrollment's last step: the machine reported in, so record
// it. A name already taken is an error rather than a second row.
func (s *Store) AddHost(ctx context.Context, h *Host) (int64, error) {
	facts := string(h.Facts)
	if facts == "" {
		facts = "{}"
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO hosts(name, addr, port, user, host_key, facts, rebuild_script) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		h.Name, h.Addr, h.Port, h.User, h.HostKey, facts, h.RebuildScript)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return 0, fmt.Errorf("a host named %q is already enrolled", h.Name)
		}
		return 0, err
	}
	return res.LastInsertId()
}

// RemoveHost forgets a machine. Nothing on the machine changes.
func (s *Store) RemoveHost(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM hosts WHERE id = ?`, id)
	return err
}

// SetHostStatus records what the heartbeat found and returns the status
// it replaced, so the caller can tell a transition from a repeat.
func (s *Store) SetHostStatus(ctx context.Context, id int64, status string, facts json.RawMessage, agentModel string) (prev string, err error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, `SELECT status FROM hosts WHERE id = ?`, id).Scan(&prev); err != nil {
		return "", err
	}
	if facts != nil {
		_, err = tx.ExecContext(ctx,
			`UPDATE hosts SET status = ?, facts = ?, agent_model = ?, last_seen = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`,
			status, string(facts), agentModel, id)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE hosts SET status = ? WHERE id = ?`, status, id)
	}
	if err != nil {
		return "", err
	}
	return prev, tx.Commit()
}

// SetRebuildScript replaces a host's rebuild script.
func (s *Store) SetRebuildScript(ctx context.Context, id int64, script string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE hosts SET rebuild_script = ? WHERE id = ?`, script, id)
	return err
}

// AppendRebuildScript adds lines the hub itself ran on the host.
func (s *Store) AppendRebuildScript(ctx context.Context, id int64, lines string) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE hosts SET rebuild_script = rtrim(rebuild_script, char(10)) || char(10) || ? || char(10) WHERE id = ?`, lines, id)
	return err
}

// EnrollCode is one single-use code.
type EnrollCode struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	RebuildFrom int64  `json:"rebuildFrom,omitempty"`
	Expires     string `json:"expires"`
}

// NewEnrollCode mints a code that is good once, for fifteen minutes.
func (s *Store) NewEnrollCode(ctx context.Context, code, name string, rebuildFrom int64) (*EnrollCode, error) {
	exp := time.Now().Add(15 * time.Minute).UTC().Format(time.RFC3339)
	var rf any
	if rebuildFrom != 0 {
		rf = rebuildFrom
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO enroll_codes(code, name, rebuild_from, expires) VALUES (?, ?, ?, ?)`, code, name, rf, exp)
	if err != nil {
		return nil, err
	}
	return &EnrollCode{Code: code, Name: name, RebuildFrom: rebuildFrom, Expires: exp}, nil
}

// EnrollCode returns a live code, or nil if it is unknown, used or expired.
func (s *Store) EnrollCode(ctx context.Context, code string) (*EnrollCode, error) {
	var c EnrollCode
	var rf sql.NullInt64
	err := s.RO.QueryRowContext(ctx,
		`SELECT code, name, rebuild_from, expires FROM enroll_codes WHERE code = ? AND used = 0 AND expires > ?`,
		code, time.Now().UTC().Format(time.RFC3339)).Scan(&c.Code, &c.Name, &rf, &c.Expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.RebuildFrom = rf.Int64
	return &c, nil
}

// UseEnrollCode spends a code. False if it was already spent or gone.
func (s *Store) UseEnrollCode(ctx context.Context, code string) (bool, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE enroll_codes SET used = 1 WHERE code = ? AND used = 0 AND expires > ?`,
		code, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// Metric is one sweep's numbers for one host.
type Metric struct {
	At       string          `json:"at"`
	MemUsed  int64           `json:"memUsed"`
	MemTotal int64           `json:"memTotal"`
	Load1    float64         `json:"load1"`
	GPUBusy  float64         `json:"gpuBusy"`
	Mounts   json.RawMessage `json:"mounts"` // {"/": freeBytes, ...}
}

// RecordMetric keeps one sweep's reading.
func (s *Store) RecordMetric(ctx context.Context, hostID int64, m Metric) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO metrics(host_id, mem_used, mem_total, load1, gpu_busy, mounts) VALUES (?, ?, ?, ?, ?, ?)`,
		hostID, m.MemUsed, m.MemTotal, m.Load1, int(m.GPUBusy), string(m.Mounts))
	return err
}

// RollupMetrics averages the raw rows into metrics_hourly and drops raw
// rows older than two days. Mount free space is averaged per mountpoint.
// Only complete hours a host has not been rolled up for are read: the
// raw table holds two days, the rollup runs hourly, and re-averaging
// what is already averaged is the same answer for forty-eight times the
// work.
func (s *Store) RollupMetrics(ctx context.Context) error {
	rows, err := s.RO.QueryContext(ctx,
		`SELECT host_id, strftime('%Y-%m-%dT%H:00:00Z', at) AS hour, mem_used, mem_total, load1, gpu_busy, mounts
		 FROM metrics m
		 WHERE at < strftime('%Y-%m-%dT%H:00:00Z','now')
		   AND at >= COALESCE((SELECT strftime('%Y-%m-%dT%H:00:00Z', MAX(hour), '+1 hour') FROM metrics_hourly h WHERE h.host_id = m.host_id), '')
		 ORDER BY host_id, hour`)
	if err != nil {
		return err
	}
	type acc struct {
		n                int
		mem, total, load float64
		gpu              float64
		mounts           map[string]float64
	}
	type key struct {
		host int64
		hour string
	}
	accs := map[key]*acc{}
	for rows.Next() {
		var host int64
		var hour, mounts string
		var mu, mt, gpu int64
		var load float64
		if err := rows.Scan(&host, &hour, &mu, &mt, &load, &gpu, &mounts); err != nil {
			rows.Close()
			return err
		}
		k := key{host, hour}
		a := accs[k]
		if a == nil {
			a = &acc{mounts: map[string]float64{}}
			accs[k] = a
		}
		a.n++
		a.mem += float64(mu)
		a.total += float64(mt)
		a.load += load
		a.gpu += float64(gpu)
		var m map[string]float64
		if json.Unmarshal([]byte(mounts), &m) == nil {
			for p, v := range m {
				a.mounts[p] += v
			}
		}
	}
	rows.Close()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, a := range accs {
		n := float64(a.n)
		for p := range a.mounts {
			a.mounts[p] /= n
		}
		mb, _ := json.Marshal(a.mounts)
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO metrics_hourly(host_id, hour, mem_used, mem_total, load1, gpu_busy, mounts) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			k.host, k.hour, int64(a.mem/n), int64(a.total/n), a.load/n, a.gpu/n, string(mb)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM metrics WHERE at < strftime('%Y-%m-%dT%H:%M:%fZ','now','-2 days')`); err != nil {
		return err
	}
	return tx.Commit()
}

// HostMetrics returns the raw sweeps of the last day and the hourly
// rollup before it, oldest first, as one series.
func (s *Store) HostMetrics(ctx context.Context, hostID int64, hours int) ([]Metric, error) {
	since := time.Now().Add(-time.Duration(hours) * time.Hour).UTC().Format(time.RFC3339)
	rows, err := s.RO.QueryContext(ctx,
		`SELECT hour, mem_used, mem_total, load1, gpu_busy, mounts FROM metrics_hourly WHERE host_id = ? AND hour >= ?
		 UNION ALL
		 SELECT at, mem_used, mem_total, load1, gpu_busy, mounts FROM metrics WHERE host_id = ? AND at >= ?
		 ORDER BY 1`, hostID, since, hostID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Metric{}
	for rows.Next() {
		var m Metric
		var mounts string
		if err := rows.Scan(&m.At, &m.MemUsed, &m.MemTotal, &m.Load1, &m.GPUBusy, &mounts); err != nil {
			return nil, err
		}
		m.Mounts = json.RawMessage(mounts)
		out = append(out, m)
	}
	return out, rows.Err()
}

// Usage is token cost for one model reply, on the hub's account of a
// job's round (RecordUsage) or summed into an hour (HostUsage,
// UsageTotals) — the counters an omp `message_end` reports: input is
// prompt tokens minus what came off cache, cacheRead/cacheWrite the
// cache's own share, and cost the provider's own USD estimate.
type Usage struct {
	At         string  `json:"at"`
	Model      string  `json:"model,omitempty"`
	Input      int64   `json:"input"`
	Output     int64   `json:"output"`
	CacheRead  int64   `json:"cacheRead"`
	CacheWrite int64   `json:"cacheWrite"`
	Cost       float64 `json:"cost"`
	Calls      int64   `json:"calls,omitempty"`
}

// RecordUsage keeps one assistant reply's token counts, attributed to
// the job's host and the job itself.
func (s *Store) RecordUsage(ctx context.Context, hostID, jobID int64, u Usage) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO usage(host_id, job_id, model, input, output, cache_read, cache_write, cost) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		hostID, jobID, u.Model, u.Input, u.Output, u.CacheRead, u.CacheWrite, u.Cost)
	return err
}

// RollupUsage sums the raw rows into usage_hourly and drops raw rows
// older than two days, the same shape as RollupMetrics: only the hours
// not yet rolled up for a host are read.
func (s *Store) RollupUsage(ctx context.Context) error {
	rows, err := s.RO.QueryContext(ctx,
		`SELECT host_id, strftime('%Y-%m-%dT%H:00:00Z', at) AS hour, input, output, cache_read, cache_write, cost
		 FROM usage m
		 WHERE at < strftime('%Y-%m-%dT%H:00:00Z','now')
		   AND at >= COALESCE((SELECT strftime('%Y-%m-%dT%H:00:00Z', MAX(hour), '+1 hour') FROM usage_hourly h WHERE h.host_id = m.host_id), '')
		 ORDER BY host_id, hour`)
	if err != nil {
		return err
	}
	type acc struct {
		n, input, output, cacheRead, cacheWrite int64
		cost                                    float64
	}
	type key struct {
		host int64
		hour string
	}
	accs := map[key]*acc{}
	for rows.Next() {
		var host int64
		var hour string
		var input, output, cacheRead, cacheWrite int64
		var cost float64
		if err := rows.Scan(&host, &hour, &input, &output, &cacheRead, &cacheWrite, &cost); err != nil {
			rows.Close()
			return err
		}
		k := key{host, hour}
		a := accs[k]
		if a == nil {
			a = &acc{}
			accs[k] = a
		}
		a.n++
		a.input += input
		a.output += output
		a.cacheRead += cacheRead
		a.cacheWrite += cacheWrite
		a.cost += cost
	}
	rows.Close()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, a := range accs {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO usage_hourly(host_id, hour, input, output, cache_read, cache_write, cost, calls) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			k.host, k.hour, a.input, a.output, a.cacheRead, a.cacheWrite, a.cost, a.n); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM usage WHERE at < strftime('%Y-%m-%dT%H:%M:%fZ','now','-2 days')`); err != nil {
		return err
	}
	return tx.Commit()
}

// HostUsage returns one host's usage, bucketed by hour, oldest first:
// the hourly rollup before the last day, the raw rows since — the same
// two-tier series HostMetrics reads.
func (s *Store) HostUsage(ctx context.Context, hostID int64, hours int) ([]Usage, error) {
	since := time.Now().Add(-time.Duration(hours) * time.Hour).UTC().Format(time.RFC3339)
	rows, err := s.RO.QueryContext(ctx,
		`SELECT hour, input, output, cache_read, cache_write, cost, calls FROM usage_hourly WHERE host_id = ? AND hour >= ?
		 UNION ALL
		 SELECT strftime('%Y-%m-%dT%H:00:00Z', at), input, output, cache_read, cache_write, cost, 1 FROM usage WHERE host_id = ? AND at >= ?
		 ORDER BY 1`, hostID, since, hostID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Usage{}
	for rows.Next() {
		var u Usage
		if err := rows.Scan(&u.At, &u.Input, &u.Output, &u.CacheRead, &u.CacheWrite, &u.Cost, &u.Calls); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UsageTotals sums every host's usage over the window, newest-heaviest
// first by total tokens — the fleet-wide comparison the Usage tab
// leads with, to see whether dispatch actually spreads across remotes.
// A host with no rows in the window still appears, at zero, so an idle
// remote is as visible as a busy one.
func (s *Store) UsageTotals(ctx context.Context, hours int) ([]HostUsage, error) {
	since := time.Now().Add(-time.Duration(hours) * time.Hour).UTC().Format(time.RFC3339)
	rows, err := s.RO.QueryContext(ctx,
		`SELECT h.id, h.name,
			COALESCE((SELECT SUM(input) FROM usage_hourly u WHERE u.host_id = h.id AND u.hour >= ?), 0)
				+ COALESCE((SELECT SUM(input) FROM usage u WHERE u.host_id = h.id AND u.at >= ?), 0),
			COALESCE((SELECT SUM(output) FROM usage_hourly u WHERE u.host_id = h.id AND u.hour >= ?), 0)
				+ COALESCE((SELECT SUM(output) FROM usage u WHERE u.host_id = h.id AND u.at >= ?), 0),
			COALESCE((SELECT SUM(cache_read) FROM usage_hourly u WHERE u.host_id = h.id AND u.hour >= ?), 0)
				+ COALESCE((SELECT SUM(cache_read) FROM usage u WHERE u.host_id = h.id AND u.at >= ?), 0),
			COALESCE((SELECT SUM(cache_write) FROM usage_hourly u WHERE u.host_id = h.id AND u.hour >= ?), 0)
				+ COALESCE((SELECT SUM(cache_write) FROM usage u WHERE u.host_id = h.id AND u.at >= ?), 0),
			COALESCE((SELECT SUM(cost) FROM usage_hourly u WHERE u.host_id = h.id AND u.hour >= ?), 0)
				+ COALESCE((SELECT SUM(cost) FROM usage u WHERE u.host_id = h.id AND u.at >= ?), 0),
			COALESCE((SELECT SUM(calls) FROM usage_hourly u WHERE u.host_id = h.id AND u.hour >= ?), 0)
				+ COALESCE((SELECT COUNT(*) FROM usage u WHERE u.host_id = h.id AND u.at >= ?), 0)
		 FROM hosts h
		 ORDER BY 3 + 4 DESC, h.name`,
		since, since, since, since, since, since, since, since, since, since, since, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HostUsage{}
	for rows.Next() {
		var u HostUsage
		if err := rows.Scan(&u.HostID, &u.Host, &u.Input, &u.Output, &u.CacheRead, &u.CacheWrite, &u.Cost, &u.Calls); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// HostUsage is one host's usage total over a window, named for the
// panel: which remote, and how much of the fleet's tokens it carried.
type HostUsage struct {
	HostID     int64   `json:"hostId"`
	Host       string  `json:"host"`
	Input      int64   `json:"input"`
	Output     int64   `json:"output"`
	CacheRead  int64   `json:"cacheRead"`
	CacheWrite int64   `json:"cacheWrite"`
	Cost       float64 `json:"cost"`
	Calls      int64   `json:"calls"`
}

// SetVaultToken records the token this host presents to the vault proxy;
// empty means revoked, and the proxy answers nothing until the next
// delivery mints another.
func (s *Store) SetVaultToken(ctx context.Context, id int64, token string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE hosts SET vault_token = ? WHERE id = ?`, token, id)
	return err
}

// SetCredentialsRevoked records when Revoke last ran, or clears it (an
// empty at) once Update credentials mints a fresh token (H-9).
func (s *Store) SetCredentialsRevoked(ctx context.Context, id int64, at string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE hosts SET credentials_revoked_at = ? WHERE id = ?`, at, id)
	return err
}

// SetHostAddr re-pins the address the hub reaches a host at — after
// the machine was confirmed there with its enrolled key. Nothing else
// about the record changes; the host key is what identifies it.
func (s *Store) SetHostAddr(ctx context.Context, id int64, addr string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE hosts SET addr = ? WHERE id = ?`, addr, id)
	return err
}
