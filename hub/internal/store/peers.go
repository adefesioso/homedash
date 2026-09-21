package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"sort"
	"strings"
	"time"
)

const peerSchema = `
CREATE TABLE IF NOT EXISTS peers (
	id             TEXT PRIMARY KEY,
	name           TEXT NOT NULL DEFAULT '',
	approved       INTEGER NOT NULL DEFAULT 0,
	max_concurrent INTEGER NOT NULL DEFAULT 1,
	per_hour       INTEGER NOT NULL DEFAULT 20,
	first_seen     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	last_seen      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE TABLE IF NOT EXISTS peer_jobs (
	id        INTEGER PRIMARY KEY,
	peer_id   TEXT NOT NULL,
	direction TEXT NOT NULL CHECK (direction IN ('sent','served')),
	model     TEXT NOT NULL,
	at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	accepted  INTEGER NOT NULL,
	finished  INTEGER NOT NULL DEFAULT 0,
	ttft_ms   INTEGER,
	reason    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS peer_jobs_peer ON peer_jobs(peer_id, at);
CREATE TABLE IF NOT EXISTS services (
	name      TEXT PRIMARY KEY,
	host_id   INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
	port      INTEGER NOT NULL,
	peers     TEXT NOT NULL DEFAULT '',
	created   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE TABLE IF NOT EXISTS fronts (
	peer_id   TEXT NOT NULL,
	service   TEXT NOT NULL,
	approved  INTEGER NOT NULL DEFAULT 0,
	hostname  TEXT NOT NULL DEFAULT '',
	created   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	PRIMARY KEY (peer_id, service)
);
CREATE TABLE IF NOT EXISTS peer_bytes (
	peer_id   TEXT NOT NULL,
	service   TEXT NOT NULL,
	direction TEXT NOT NULL CHECK (direction IN ('fronted','origin')),
	hour      TEXT NOT NULL,
	bytes     INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (peer_id, service, direction, hour)
);
`

// Peer is one hub discovered in the space, as this hub controls it.
type Peer struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Approved      bool   `json:"approved"`
	MaxConcurrent int    `json:"maxConcurrent"`
	PerHour       int    `json:"perHour"`
	FirstSeen     string `json:"firstSeen"`
	LastSeen      string `json:"lastSeen"`
}

// SeenPeer records a discovery (or refreshes one). New peers take the
// space-wide defaults; approval is a person's act.
func (s *Store) SeenPeer(ctx context.Context, id, name string, approve bool, maxConcurrent, perHour int) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO peers(id, name, approved, max_concurrent, per_hour) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET name = CASE WHEN excluded.name != '' THEN excluded.name ELSE peers.name END, last_seen = strftime('%Y-%m-%dT%H:%M:%fZ','now')`,
		id, name, approve, maxConcurrent, perHour)
	return err
}

// Peers lists the space.
func (s *Store) Peers(ctx context.Context) ([]Peer, error) {
	rows, err := s.RO.QueryContext(ctx, `SELECT id, name, approved, max_concurrent, per_hour, first_seen, last_seen FROM peers ORDER BY last_seen DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Peer{}
	for rows.Next() {
		var p Peer
		if err := rows.Scan(&p.ID, &p.Name, &p.Approved, &p.MaxConcurrent, &p.PerHour, &p.FirstSeen, &p.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Peer reads one.
func (s *Store) Peer(ctx context.Context, id string) (*Peer, error) {
	var p Peer
	err := s.RO.QueryRowContext(ctx, `SELECT id, name, approved, max_concurrent, per_hour, first_seen, last_seen FROM peers WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &p.Approved, &p.MaxConcurrent, &p.PerHour, &p.FirstSeen, &p.LastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("unknown peer")
	}
	return &p, err
}

// SetPeer writes the controls.
func (s *Store) SetPeer(ctx context.Context, id string, approved bool, maxConcurrent, perHour int) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE peers SET approved = ?, max_concurrent = ?, per_hour = ? WHERE id = ?`, approved, maxConcurrent, perHour, id)
	return err
}

// ForgetPeer drops a row; discovery will bring it back if it is still there.
func (s *Store) ForgetPeer(ctx context.Context, id string) error {
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM fronts WHERE peer_id = ?`, id); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM peers WHERE id = ?`, id)
	return err
}

// RecordPeerJob keeps one exchange and returns its row id, for the
// finish and first-token updates.
func (s *Store) RecordPeerJob(ctx context.Context, peerID, direction, model string, accepted bool, reason string) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO peer_jobs(peer_id, direction, model, accepted, reason) VALUES (?, ?, ?, ?, ?)`,
		peerID, direction, model, accepted, reason)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishPeerJob records how an accepted exchange ended.
func (s *Store) FinishPeerJob(ctx context.Context, id int64, finished bool, ttft time.Duration) error {
	var t any
	if ttft > 0 {
		t = ttft.Milliseconds()
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE peer_jobs SET finished = ?, ttft_ms = COALESCE(?, ttft_ms) WHERE id = ?`, finished, t, id)
	return err
}

// ServedInLastHour is the quota check.
func (s *Store) ServedInLastHour(ctx context.Context, peerID string) (int, error) {
	var n int
	err := s.RO.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM peer_jobs WHERE peer_id = ? AND direction = 'served' AND accepted = 1 AND at > strftime('%Y-%m-%dT%H:%M:%fZ','now','-1 hour')`, peerID).Scan(&n)
	return n, err
}

// ServedInLastHourAll counts accepted jobs from the whole space in the
// last hour: the hub-wide allowance, whatever the peers' own add up to.
func (s *Store) ServedInLastHourAll(ctx context.Context) (int, error) {
	var n int
	err := s.RO.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM peer_jobs WHERE direction = 'served' AND accepted = 1 AND at > strftime('%Y-%m-%dT%H:%M:%fZ','now','-1 hour')`).Scan(&n)
	return n, err
}

// Record is the three decayed numbers the router chooses by.
type Record struct {
	Accepted   float64 `json:"accepted"`   // share of jobs accepted rather than refused
	Finished   float64 `json:"finished"`   // share of accepted jobs that finished
	MedianTTFT float64 `json:"medianTTFT"` // ms
	Samples    int     `json:"samples"`
}

// PeerRecord computes the record from the last week of sent jobs, each
// weighted by half per two days of age, so last week stops counting.
func (s *Store) PeerRecord(ctx context.Context, peerID string) (Record, error) {
	rows, err := s.RO.QueryContext(ctx,
		`SELECT at, accepted, finished, ttft_ms FROM peer_jobs WHERE peer_id = ? AND direction = 'sent' AND at > strftime('%Y-%m-%dT%H:%M:%fZ','now','-7 days')`, peerID)
	if err != nil {
		return Record{}, err
	}
	defer rows.Close()
	var wAll, wAcc, wAccAll, wFin float64
	type tt struct{ ms, w float64 }
	var ttfts []tt
	for rows.Next() {
		var at string
		var accepted, finished bool
		var ttft sql.NullInt64
		if err := rows.Scan(&at, &accepted, &finished, &ttft); err != nil {
			return Record{}, err
		}
		t, _ := time.Parse(time.RFC3339Nano, at)
		w := math.Pow(0.5, time.Since(t).Hours()/48)
		wAll += w
		if accepted {
			wAcc += w
			wAccAll += w
			if finished {
				wFin += w
			}
			if ttft.Valid {
				ttfts = append(ttfts, tt{float64(ttft.Int64), w})
			}
		}
	}
	r := Record{Samples: len(ttfts)}
	if wAll > 0 {
		r.Accepted = wAcc / wAll
	}
	if wAccAll > 0 {
		r.Finished = wFin / wAccAll
	}
	if len(ttfts) > 0 {
		sort.Slice(ttfts, func(i, j int) bool { return ttfts[i].ms < ttfts[j].ms })
		var total, acc float64
		for _, x := range ttfts {
			total += x.w
		}
		for _, x := range ttfts {
			acc += x.w
			if acc >= total/2 {
				r.MedianTTFT = x.ms
				break
			}
		}
	}
	return r, rows.Err()
}

// Counts is the score: both directions, by model, over a day, a week
// and all time — and the bytes carried for a peer's services (fronted)
// and by that peer for this hub's (origin), the same three ways.
type Counts struct {
	Sent    map[string][3]int `json:"sent"`   // model → [day, week, all]
	Served  map[string][3]int `json:"served"` // model → [day, week, all]
	Fronted [3]int64          `json:"fronted"`
	Origin  [3]int64          `json:"origin"`
}

// PeerCounts totals the accepted exchanges with one peer.
func (s *Store) PeerCounts(ctx context.Context, peerID string) (Counts, error) {
	rows, err := s.RO.QueryContext(ctx,
		`SELECT direction, model, at FROM peer_jobs WHERE peer_id = ? AND accepted = 1`, peerID)
	if err != nil {
		return Counts{}, err
	}
	defer rows.Close()
	c := Counts{Sent: map[string][3]int{}, Served: map[string][3]int{}}
	for rows.Next() {
		var dir, model, at string
		if err := rows.Scan(&dir, &model, &at); err != nil {
			return Counts{}, err
		}
		t, _ := time.Parse(time.RFC3339Nano, at)
		age := time.Since(t)
		m := c.Sent
		if dir == "served" {
			m = c.Served
		}
		v := m[model]
		v[2]++
		if age < 7*24*time.Hour {
			v[1]++
		}
		if age < 24*time.Hour {
			v[0]++
		}
		m[model] = v
	}
	if err := rows.Err(); err != nil {
		return Counts{}, err
	}
	brows, err := s.RO.QueryContext(ctx, `SELECT direction, hour, bytes FROM peer_bytes WHERE peer_id = ?`, peerID)
	if err != nil {
		return Counts{}, err
	}
	defer brows.Close()
	for brows.Next() {
		var dir, hour string
		var n int64
		if err := brows.Scan(&dir, &hour, &n); err != nil {
			return Counts{}, err
		}
		t, _ := time.Parse("2006-01-02T15", hour)
		age := time.Since(t)
		v := &c.Fronted
		if dir == "origin" {
			v = &c.Origin
		}
		v[2] += n
		if age < 7*24*time.Hour {
			v[1] += n
		}
		if age < 24*time.Hour {
			v[0] += n
		}
	}
	return c, brows.Err()
}

// AddPeerBytes counts bytes copied for one peer and service in this hour.
func (s *Store) AddPeerBytes(ctx context.Context, peerID, service, direction string, n int64) error {
	if n <= 0 {
		return nil
	}
	hour := time.Now().UTC().Format("2006-01-02T15")
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO peer_bytes(peer_id, service, direction, hour, bytes) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(peer_id, service, direction, hour) DO UPDATE SET bytes = bytes + excluded.bytes`,
		peerID, service, direction, hour, n)
	return err
}

// --- published services ----------------------------------------------

// Service is one port on one machine this hub owns, and the peers that
// may front it.
type Service struct {
	Name   string   `json:"name"`
	HostID int64    `json:"hostId"`
	Host   string   `json:"host"`
	Port   int      `json:"port"`
	Peers  []string `json:"peers"`
}

const serviceCols = `s.name, s.host_id, h.name, s.port, s.peers`

func scanService(sc interface{ Scan(...any) error }) (*Service, error) {
	var sv Service
	var peers string
	if err := sc.Scan(&sv.Name, &sv.HostID, &sv.Host, &sv.Port, &peers); err != nil {
		return nil, err
	}
	sv.Peers = splitList(peers)
	return &sv, nil
}

// Services lists what this hub has published.
func (s *Store) Services(ctx context.Context) ([]Service, error) {
	rows, err := s.RO.QueryContext(ctx, `SELECT `+serviceCols+` FROM services s JOIN hosts h ON h.id = s.host_id ORDER BY s.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Service{}
	for rows.Next() {
		sv, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sv)
	}
	return out, rows.Err()
}

// Service reads one by name.
func (s *Store) Service(ctx context.Context, name string) (*Service, error) {
	sv, err := scanService(s.RO.QueryRowContext(ctx, `SELECT `+serviceCols+` FROM services s JOIN hosts h ON h.id = s.host_id WHERE s.name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("no such service")
	}
	return sv, err
}

// PublishService records or replaces a service: a port on a host this
// hub owns, offered to exactly these peers.
func (s *Store) PublishService(ctx context.Context, name string, hostID int64, port int, peers []string) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO services(name, host_id, port, peers) VALUES (?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET host_id = excluded.host_id, port = excluded.port, peers = excluded.peers`,
		name, hostID, port, joinList(peers))
	return err
}

// UnpublishService forgets one; it leaves the peers' next offer.
func (s *Store) UnpublishService(ctx context.Context, name string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM services WHERE name = ?`, name)
	return err
}

// --- fronts ------------------------------------------------------------

// Front is a peer's service this hub may serve: as a link, or under a
// hostname as an ingress.
type Front struct {
	PeerID   string `json:"peerId"`
	Service  string `json:"service"`
	Approved bool   `json:"approved"`
	Hostname string `json:"hostname"`
}

// SeenFront records a service a peer's offer named, unapproved, if this
// hub has not seen it from that peer before.
func (s *Store) SeenFront(ctx context.Context, peerID, service string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO fronts(peer_id, service) VALUES (?, ?)`, peerID, service)
	return err
}

// SetFront writes the controls: approved, and the hostname if it is an
// ingress rather than a link.
func (s *Store) SetFront(ctx context.Context, peerID, service string, approved bool, hostname string) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO fronts(peer_id, service, approved, hostname) VALUES (?, ?, ?, ?)
		 ON CONFLICT(peer_id, service) DO UPDATE SET approved = excluded.approved, hostname = excluded.hostname`,
		peerID, service, approved, hostname)
	return err
}

// Front reads one.
func (s *Store) Front(ctx context.Context, peerID, service string) (*Front, error) {
	var f Front
	err := s.RO.QueryRowContext(ctx, `SELECT peer_id, service, approved, hostname FROM fronts WHERE peer_id = ? AND service = ?`, peerID, service).
		Scan(&f.PeerID, &f.Service, &f.Approved, &f.Hostname)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("no such front")
	}
	return &f, err
}

// Fronts lists every front, optionally one peer's.
func (s *Store) Fronts(ctx context.Context, peerID string) ([]Front, error) {
	q := `SELECT peer_id, service, approved, hostname FROM fronts`
	var args []any
	if peerID != "" {
		q += ` WHERE peer_id = ?`
		args = append(args, peerID)
	}
	rows, err := s.RO.QueryContext(ctx, q+` ORDER BY peer_id, service`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Front{}
	for rows.Next() {
		var f Front
		if err := rows.Scan(&f.PeerID, &f.Service, &f.Approved, &f.Hostname); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FrontByHostname resolves an ingress hostname to its approved front.
func (s *Store) FrontByHostname(ctx context.Context, hostname string) (*Front, error) {
	var f Front
	err := s.RO.QueryRowContext(ctx, `SELECT peer_id, service, approved, hostname FROM fronts WHERE hostname = ? AND approved = 1`, hostname).
		Scan(&f.PeerID, &f.Service, &f.Approved, &f.Hostname)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("no such front")
	}
	return &f, err
}

func splitList(s string) []string {
	out := []string{}
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

func joinList(xs []string) string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if x = strings.TrimSpace(x); x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return strings.Join(out, ",")
}
