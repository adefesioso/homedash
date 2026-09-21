package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const deviceSchema = `
-- A device is anything a remote can see that is not enrolled, known by
-- its hardware address. The guess columns are the hub's; name and
-- user_kind are yours and are never overwritten by a scan.
CREATE TABLE IF NOT EXISTS devices (
	id         INTEGER PRIMARY KEY,
	kind       TEXT NOT NULL CHECK (kind IN ('lan','wifi','bt')),
	addr       TEXT NOT NULL,
	guess_name TEXT NOT NULL DEFAULT '',
	guess_kind TEXT NOT NULL DEFAULT '',
	vendor     TEXT NOT NULL DEFAULT '',
	name       TEXT NOT NULL DEFAULT '',
	user_kind  TEXT NOT NULL DEFAULT '',
	first_seen TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	last_seen  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	UNIQUE (kind, addr)
);
-- One row per device per remote that has seen it: when, and what it saw.
CREATE TABLE IF NOT EXISTS sightings (
	device_id  INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	host_id    INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
	seen       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	detail     TEXT NOT NULL DEFAULT '{}',
	PRIMARY KEY (device_id, host_id)
);
-- What each remote's last scan could see: a slice is 1 when the machine
-- has the interface and tools for it. error is why the scan failed, or ''.
CREATE TABLE IF NOT EXISTS host_scans (
	host_id    INTEGER PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
	at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	lan        INTEGER NOT NULL DEFAULT 0,
	wifi       INTEGER NOT NULL DEFAULT 0,
	bt         INTEGER NOT NULL DEFAULT 0,
	error      TEXT NOT NULL DEFAULT ''
);
`

// HostScan is what one remote's last scan could see.
type HostScan struct {
	HostID   int64  `json:"hostId"`
	HostName string `json:"host"`
	// Status is the host's current status ("online"/"offline"), not the
	// scan's: a host that has gone offline since its last scan still
	// shows that scan's rows, marked with what the host is now (N-1).
	Status string `json:"status"`
	At     string `json:"at"`
	LAN    bool   `json:"lan"`
	WiFi   bool   `json:"wifi"`
	BT     bool   `json:"bt"`
	Error  string `json:"error"`
}

// SetHostScan records the outcome of one remote's scan.
func (s *Store) SetHostScan(ctx context.Context, hostID int64, lan, wifi, bt bool, errText string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO host_scans(host_id, lan, wifi, bt, error) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(host_id) DO UPDATE SET at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), lan = excluded.lan, wifi = excluded.wifi, bt = excluded.bt, error = excluded.error`,
		hostID, lan, wifi, bt, errText)
	return err
}

// HostScans returns every remote's last scan outcome.
func (s *Store) HostScans(ctx context.Context) ([]HostScan, error) {
	rows, err := s.RO.QueryContext(ctx, `SELECT s.host_id, h.name, h.status, s.at, s.lan, s.wifi, s.bt, s.error
		FROM host_scans s JOIN hosts h ON h.id = s.host_id ORDER BY h.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HostScan{}
	for rows.Next() {
		var h HostScan
		if err := rows.Scan(&h.HostID, &h.HostName, &h.Status, &h.At, &h.LAN, &h.WiFi, &h.BT, &h.Error); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// Device is one row of devices with its sightings attached.
type Device struct {
	ID        int64      `json:"id"`
	Kind      string     `json:"kind"`
	Addr      string     `json:"addr"`
	GuessName string     `json:"guessName"`
	GuessKind string     `json:"guessKind"`
	Vendor    string     `json:"vendor"`
	Name      string     `json:"name"`
	UserKind  string     `json:"userKind"`
	FirstSeen string     `json:"firstSeen"`
	LastSeen  string     `json:"lastSeen"`
	Sightings []Sighting `json:"sightings"`
}

// Sighting is one remote's last view of a device.
type Sighting struct {
	HostID   int64           `json:"hostId"`
	HostName string          `json:"host"`
	Seen     string          `json:"seen"`
	Detail   json.RawMessage `json:"detail"`
}

// Devices returns every device, most recently seen first, with sightings.
func (s *Store) Devices(ctx context.Context) ([]Device, error) {
	rows, err := s.RO.QueryContext(ctx, `SELECT id, kind, addr, guess_name, guess_kind, vendor, name, user_kind, first_seen, last_seen
		FROM devices ORDER BY last_seen DESC, id`)
	if err != nil {
		return nil, err
	}
	out := []Device{}
	byID := map[int64]int{}
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.Kind, &d.Addr, &d.GuessName, &d.GuessKind, &d.Vendor, &d.Name, &d.UserKind, &d.FirstSeen, &d.LastSeen); err != nil {
			rows.Close()
			return nil, err
		}
		d.Sightings = []Sighting{}
		byID[d.ID] = len(out)
		out = append(out, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	srows, err := s.RO.QueryContext(ctx, `SELECT s.device_id, s.host_id, COALESCE(h.name, ''), s.seen, s.detail
		FROM sightings s LEFT JOIN hosts h ON h.id = s.host_id ORDER BY s.seen DESC`)
	if err != nil {
		return nil, err
	}
	defer srows.Close()
	for srows.Next() {
		var id int64
		var sg Sighting
		var detail string
		if err := srows.Scan(&id, &sg.HostID, &sg.HostName, &sg.Seen, &detail); err != nil {
			return nil, err
		}
		sg.Detail = json.RawMessage(detail)
		if i, ok := byID[id]; ok {
			out[i].Sightings = append(out[i].Sightings, sg)
		}
	}
	return out, srows.Err()
}

// Device resolves a device by id, or by "kind:addr".
func (s *Store) Device(ctx context.Context, ref string) (*Device, error) {
	ds, err := s.Devices(ctx)
	if err != nil {
		return nil, err
	}
	id, _ := strconv.ParseInt(ref, 10, 64)
	for i := range ds {
		if ds[i].ID == id || strings.EqualFold(ds[i].Kind+":"+ds[i].Addr, ref) {
			return &ds[i], nil
		}
	}
	return nil, fmt.Errorf("no such device %q", ref)
}

// Seen is one device a scan saw: what Merge hands SightAll.
type Seen struct {
	Kind, Addr string
	Detail     json.RawMessage
}

// SightAll records one remote seeing every device in seen, now, in one
// transaction — one commit per scan, not one per device — creating
// each device on its first sighting. Returns, in order, the device ids
// and whether each was new.
func (s *Store) SightAll(ctx context.Context, hostID int64, seen []Seen) (ids []int64, created []bool, err error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	find, err := tx.PrepareContext(ctx, `SELECT id FROM devices WHERE kind = ? AND addr = ?`)
	if err != nil {
		return nil, nil, err
	}
	defer find.Close()
	add, err := tx.PrepareContext(ctx, `INSERT INTO devices(kind, addr) VALUES (?, ?)`)
	if err != nil {
		return nil, nil, err
	}
	defer add.Close()
	touch, err := tx.PrepareContext(ctx, `UPDATE devices SET last_seen = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`)
	if err != nil {
		return nil, nil, err
	}
	defer touch.Close()
	sight, err := tx.PrepareContext(ctx, `INSERT INTO sightings(device_id, host_id, detail) VALUES (?, ?, ?)
		ON CONFLICT(device_id, host_id) DO UPDATE SET seen = strftime('%Y-%m-%dT%H:%M:%fZ','now'), detail = excluded.detail`)
	if err != nil {
		return nil, nil, err
	}
	defer sight.Close()
	ids = make([]int64, 0, len(seen))
	created = make([]bool, 0, len(seen))
	for _, d := range seen {
		addr := strings.ToLower(d.Addr)
		var id int64
		isNew := false
		switch err := find.QueryRowContext(ctx, d.Kind, addr).Scan(&id); {
		case errors.Is(err, sql.ErrNoRows):
			res, err := add.ExecContext(ctx, d.Kind, addr)
			if err != nil {
				return nil, nil, err
			}
			id, _ = res.LastInsertId()
			isNew = true
		case err != nil:
			return nil, nil, err
		default:
			if _, err := touch.ExecContext(ctx, id); err != nil {
				return nil, nil, err
			}
		}
		detail := d.Detail
		if len(detail) == 0 {
			detail = json.RawMessage("{}")
		}
		if _, err := sight.ExecContext(ctx, id, hostID, string(detail)); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		created = append(created, isNew)
	}
	return ids, created, tx.Commit()
}

// Sight is SightAll for one device.
func (s *Store) Sight(ctx context.Context, kind, addr string, hostID int64, detail json.RawMessage) (id int64, created bool, err error) {
	ids, made, err := s.SightAll(ctx, hostID, []Seen{{Kind: kind, Addr: addr, Detail: detail}})
	if err != nil {
		return 0, false, err
	}
	return ids[0], made[0], nil
}

// SetGuess writes the hub's identification of a device.
func (s *Store) SetGuess(ctx context.Context, id int64, name, kind, vendor string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE devices SET guess_name = ?, guess_kind = ?, vendor = ? WHERE id = ?`, name, kind, vendor, id)
	return err
}

// NameDevice writes your word on a device: a name and a kind, either
// empty to clear.
func (s *Store) NameDevice(ctx context.Context, id int64, name, kind string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE devices SET name = ?, user_kind = ? WHERE id = ?`, strings.TrimSpace(name), strings.TrimSpace(kind), id)
	return err
}

// ForgetDevice drops a device and its sightings.
func (s *Store) ForgetDevice(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM devices WHERE id = ?`, id)
	return err
}

// ForgetAddrs drops every device with one of the given addresses: what
// a scan saw before its owner was known to be a host, or the hub.
func (s *Store) ForgetAddrs(ctx context.Context, addrs []string) error {
	if len(addrs) == 0 {
		return nil
	}
	args := make([]any, len(addrs))
	for i, a := range addrs {
		args[i] = strings.ToLower(a)
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM devices WHERE lower(addr) IN (?`+strings.Repeat(",?", len(addrs)-1)+`)`, args...)
	return err
}

// ForgetStale drops unnamed devices nobody has seen for days.
func (s *Store) ForgetStale(ctx context.Context, days int) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM devices WHERE name = '' AND last_seen < strftime('%Y-%m-%dT%H:%M:%fZ','now', ?)`,
		fmt.Sprintf("-%d days", days))
	return err
}
