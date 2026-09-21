package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

const authSchema = `
CREATE TABLE IF NOT EXISTS users (
	id      BLOB PRIMARY KEY,
	name    TEXT NOT NULL UNIQUE,
	role    TEXT NOT NULL CHECK (role IN ('admin','viewer')),
	created TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE TABLE IF NOT EXISTS credentials (
	id         BLOB PRIMARY KEY,
	user_id    BLOB NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	credential TEXT NOT NULL,
	created    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE TABLE IF NOT EXISTS sessions (
	token   TEXT PRIMARY KEY,
	user_id BLOB NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS invites (
	code    TEXT PRIMARY KEY,
	role    TEXT NOT NULL,
	user_id BLOB REFERENCES users(id) ON DELETE CASCADE,
	expires TEXT NOT NULL,
	used    INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS tokens (
	hash    TEXT PRIMARY KEY,
	name    TEXT NOT NULL,
	role    TEXT NOT NULL,
	created TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
`

// User is one account: a role and its passkeys.
type User struct {
	ID          []byte   `json:"-"`
	Name        string   `json:"name"`
	Role        string   `json:"role"`
	Created     string   `json:"created"`
	Credentials []string `json:"-"` // JSON, as webauthn marshals them
	Passkeys    int      `json:"passkeys"`
}

// UserCount says whether registration is still open.
func (s *Store) UserCount(ctx context.Context) (int, error) {
	var n int
	err := s.RO.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// Users lists accounts.
func (s *Store) Users(ctx context.Context) ([]User, error) {
	rows, err := s.RO.QueryContext(ctx,
		`SELECT u.id, u.name, u.role, u.created, (SELECT COUNT(*) FROM credentials c WHERE c.user_id = u.id) FROM users u ORDER BY u.created`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.Role, &u.Created, &u.Passkeys); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UserByID loads an account with its credentials.
func (s *Store) UserByID(ctx context.Context, id []byte) (*User, error) {
	var u User
	err := s.RO.QueryRowContext(ctx, `SELECT id, name, role, created FROM users WHERE id = ?`, id).Scan(&u.ID, &u.Name, &u.Role, &u.Created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("no such user")
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.RO.QueryContext(ctx, `SELECT credential FROM credentials WHERE user_id = ? ORDER BY created`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		u.Credentials = append(u.Credentials, c)
	}
	u.Passkeys = len(u.Credentials)
	return &u, rows.Err()
}

// UserByName loads an account by name.
func (s *Store) UserByName(ctx context.Context, name string) (*User, error) {
	var id []byte
	err := s.RO.QueryRowContext(ctx, `SELECT id FROM users WHERE name = ?`, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("no such user")
	}
	if err != nil {
		return nil, err
	}
	return s.UserByID(ctx, id)
}

// CreateUser makes an account with a random 64-byte handle.
func (s *Store) CreateUser(ctx context.Context, name, role string) (*User, error) {
	id := make([]byte, 64)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO users(id, name, role) VALUES (?, ?, ?)`, id, name, role); err != nil {
		return nil, err
	}
	return s.UserByID(ctx, id)
}

// AddCredential stores a passkey.
func (s *Store) AddCredential(ctx context.Context, userID, credID []byte, credential string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO credentials(id, user_id, credential) VALUES (?, ?, ?)`, credID, userID, credential)
	return err
}

// UpdateCredential replaces a passkey's record (its sign count moved).
func (s *Store) UpdateCredential(ctx context.Context, credID []byte, credential string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE credentials SET credential = ? WHERE id = ?`, credential, credID)
	return err
}

// SetRole changes an account's role.
func (s *Store) SetRole(ctx context.Context, name, role string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET role = ? WHERE name = ?`, role, name)
	return err
}

// DeleteUser removes an account, its passkeys and sessions.
func (s *Store) DeleteUser(ctx context.Context, name string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM users WHERE name = ?`, name)
	return err
}

// NewSession mints a session token good for thirty days. Only its hash
// is stored, like an API token: a read of the state DB (or a backup)
// yields no live session.
func (s *Store) NewSession(ctx context.Context, userID []byte) (string, error) {
	tok := randomToken()
	_, err := s.DB.ExecContext(ctx, `INSERT INTO sessions(token, user_id, expires) VALUES (?, ?, ?)`,
		hashToken(tok), userID, time.Now().Add(30*24*time.Hour).UTC().Format(time.RFC3339))
	return tok, err
}

// SessionUser resolves a live session to its account.
func (s *Store) SessionUser(ctx context.Context, token string) (*User, error) {
	var id []byte
	err := s.RO.QueryRowContext(ctx, `SELECT user_id FROM sessions WHERE token = ? AND expires > ?`, hashToken(token), time.Now().UTC().Format(time.RFC3339)).Scan(&id)
	if err != nil {
		return nil, errors.New("no session")
	}
	return s.UserByID(ctx, id)
}

// DeleteSession signs out.
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, hashToken(token))
	return err
}

// Invite is one single-use registration code. With a user it adds a
// passkey to that account; without, it makes a new account of the role.
type Invite struct {
	Code    string `json:"code"`
	Role    string `json:"role"`
	User    string `json:"user,omitempty"`
	Expires string `json:"expires"`
}

// NewInvite mints a code good for a day.
func (s *Store) NewInvite(ctx context.Context, role string, userID []byte) (*Invite, error) {
	code := randomToken()[:16]
	exp := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	var uid any
	if userID != nil {
		uid = userID
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO invites(code, role, user_id, expires) VALUES (?, ?, ?, ?)`, code, role, uid, exp)
	if err != nil {
		return nil, err
	}
	return &Invite{Code: code, Role: role, Expires: exp}, nil
}

// DeleteInvite revokes a code before it is used.
func (s *Store) DeleteInvite(ctx context.Context, code string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM invites WHERE code = ?`, code)
	return err
}

// Invites lists the live codes.
func (s *Store) Invites(ctx context.Context) ([]Invite, error) {
	rows, err := s.RO.QueryContext(ctx,
		`SELECT i.code, i.role, COALESCE(u.name, ''), i.expires FROM invites i LEFT JOIN users u ON u.id = i.user_id WHERE i.used = 0 AND i.expires > ? ORDER BY i.expires`,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		var i Invite
		if err := rows.Scan(&i.Code, &i.Role, &i.User, &i.Expires); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// PeekInvite checks a code is live and returns its role and account
// without spending it; the registration ceremony may still fail after
// this, so the code isn't burned until UseInvite confirms a passkey.
func (s *Store) PeekInvite(ctx context.Context, code string) (role string, userID []byte, err error) {
	err = s.RO.QueryRowContext(ctx, `SELECT role, user_id FROM invites WHERE code = ? AND used = 0 AND expires > ?`, code, time.Now().UTC().Format(time.RFC3339)).Scan(&role, &userID)
	if err != nil {
		return "", nil, errors.New("the code is not live")
	}
	return role, userID, nil
}

// UseInvite spends a live code and returns its role and account.
func (s *Store) UseInvite(ctx context.Context, code string) (role string, userID []byte, err error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT role, user_id FROM invites WHERE code = ? AND used = 0 AND expires > ?`, code, time.Now().UTC().Format(time.RFC3339)).Scan(&role, &userID)
	if err != nil {
		return "", nil, errors.New("the code is not live")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE invites SET used = 1 WHERE code = ?`, code); err != nil {
		return "", nil, err
	}
	return role, userID, tx.Commit()
}

// ErrTokenExists is returned when a name is already a live token's; a
// name identifies exactly one token, so DELETE /api/tokens/{name} is
// unambiguous about what it revokes (C-6).
var ErrTokenExists = errors.New("a token with that name already exists")

// NewToken mints an API token and returns it once; only its hash stays.
// The name is unique among live tokens (tokens_name, applied on Open).
func (s *Store) NewToken(ctx context.Context, name, role string) (string, error) {
	var n int
	if err := s.RO.QueryRowContext(ctx, `SELECT COUNT(*) FROM tokens WHERE name = ?`, name).Scan(&n); err != nil {
		return "", err
	}
	if n > 0 {
		return "", ErrTokenExists
	}
	tok := "hd_" + randomToken()
	_, err := s.DB.ExecContext(ctx, `INSERT INTO tokens(hash, name, role) VALUES (?, ?, ?)`, hashToken(tok), name, role)
	return tok, err
}

// TokenRole resolves a bearer token to its role.
func (s *Store) TokenRole(ctx context.Context, tok string) (name, role string, err error) {
	err = s.RO.QueryRowContext(ctx, `SELECT name, role FROM tokens WHERE hash = ?`, hashToken(tok)).Scan(&name, &role)
	if err != nil {
		return "", "", errors.New("no such token")
	}
	return name, role, nil
}

// Token is a row of the tokens table, for the panel.
type Token struct {
	Name    string `json:"name"`
	Role    string `json:"role"`
	Created string `json:"created"`
}

// Tokens lists names, never values.
func (s *Store) Tokens(ctx context.Context) ([]Token, error) {
	rows, err := s.RO.QueryContext(ctx, `SELECT name, role, created FROM tokens ORDER BY created`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Token{}
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.Name, &t.Role, &t.Created); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteToken revokes by name.
func (s *Store) DeleteToken(ctx context.Context, name string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM tokens WHERE name = ?`, name)
	return err
}

// dedupeTokenNames keeps the newest row for each token name and drops
// the rest — a one-time repair, run from Open before tokens_name is
// created, for a hub that minted the same name twice before it existed
// (C-6).
func dedupeTokenNames(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `DELETE FROM tokens WHERE rowid NOT IN (SELECT MAX(rowid) FROM tokens GROUP BY name)`)
	return err
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

// UserByCredential resolves a passkey's id to its account.
func (s *Store) UserByCredential(ctx context.Context, credID []byte) (*User, error) {
	var id []byte
	err := s.RO.QueryRowContext(ctx, `SELECT user_id FROM credentials WHERE id = ?`, credID).Scan(&id)
	if err != nil {
		return nil, errors.New("unknown passkey")
	}
	return s.UserByID(ctx, id)
}

// PruneEmptyUsers removes accounts that never got a passkey: a
// registration that was begun and abandoned.
func (s *Store) PruneEmptyUsers(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM users WHERE id NOT IN (SELECT user_id FROM credentials) AND created < strftime('%Y-%m-%dT%H:%M:%fZ','now','-5 minutes')`)
	return err
}
