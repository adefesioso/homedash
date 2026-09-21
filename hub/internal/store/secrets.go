package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const secretSchema = `
CREATE TABLE IF NOT EXISTS secrets (
	name       TEXT PRIMARY KEY,
	ciphertext BLOB NOT NULL,
	nonce      BLOB NOT NULL,
	hosts      TEXT NOT NULL DEFAULT '',
	created    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	updated    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
`

// ErrNoSecret is a name the hub does not hold, or one this host was not
// granted. The two are the same answer on purpose.
var ErrNoSecret = errors.New("no such secret")

// secretsKey is the AES-256 key under secrets.key in the state dir, made
// on first use and never leaving.
func (s *Store) secretsKey() ([]byte, error) {
	path := filepath.Join(filepath.Dir(s.Path), "secrets.key")
	k, err := os.ReadFile(path)
	if err == nil && len(k) == 32 {
		return k, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	k = make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, k, 0o600); err != nil {
		return nil, err
	}
	return k, nil
}

func (s *Store) secretsGCM() (cipher.AEAD, error) {
	k, err := s.secretsKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Secret is what the panel sees: never the value.
type Secret struct {
	Name    string  `json:"name"`
	Hosts   []int64 `json:"hosts"` // empty: every host
	Created string  `json:"created"`
	Updated string  `json:"updated"`
}

// SetSecret stores or replaces a value, encrypted, with its grants.
func (s *Store) SetSecret(ctx context.Context, name, value string, hosts []int64) error {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, "/ \t\n") {
		return errors.New("a secret name is one word with no slashes")
	}
	gcm, err := s.secretsGCM()
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	ct := gcm.Seal(nil, nonce, []byte(value), []byte(name))
	hs := make([]string, 0, len(hosts))
	for _, h := range hosts {
		hs = append(hs, strconv.FormatInt(h, 10))
	}
	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO secrets(name, ciphertext, nonce, hosts) VALUES (?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET ciphertext = excluded.ciphertext, nonce = excluded.nonce, hosts = excluded.hosts,
		 updated = strftime('%Y-%m-%dT%H:%M:%fZ','now')`, name, ct, nonce, strings.Join(hs, ","))
	return err
}

// DeleteSecret forgets one.
func (s *Store) DeleteSecret(ctx context.Context, name string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM secrets WHERE name = ?`, name)
	return err
}

// Secrets lists names and grants, for the panel and the hub's agent.
func (s *Store) Secrets(ctx context.Context) ([]Secret, error) {
	rows, err := s.RO.QueryContext(ctx, `SELECT name, hosts, created, updated FROM secrets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Secret{}
	for rows.Next() {
		var sec Secret
		var hosts string
		if err := rows.Scan(&sec.Name, &hosts, &sec.Created, &sec.Updated); err != nil {
			return nil, err
		}
		sec.Hosts = parseIDs(hosts)
		out = append(out, sec)
	}
	return out, rows.Err()
}

// SecretNamesFor is what one host may ask for.
func (s *Store) SecretNamesFor(ctx context.Context, hostID int64) ([]string, error) {
	all, err := s.Secrets(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, sec := range all {
		if granted(sec.Hosts, hostID) {
			out = append(out, sec.Name)
		}
	}
	return out, nil
}

// ReadSecret decrypts one value for one host, if granted. This is the
// only reader; it is called from the job door and nowhere else.
func (s *Store) ReadSecret(ctx context.Context, name string, hostID int64) (string, error) {
	var ct, nonce []byte
	var hosts string
	err := s.RO.QueryRowContext(ctx, `SELECT ciphertext, nonce, hosts FROM secrets WHERE name = ?`, name).Scan(&ct, &nonce, &hosts)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoSecret
	}
	if err != nil {
		return "", err
	}
	if !granted(parseIDs(hosts), hostID) {
		return "", ErrNoSecret
	}
	gcm, err := s.secretsGCM()
	if err != nil {
		return "", err
	}
	pt, err := gcm.Open(nil, nonce, ct, []byte(name))
	if err != nil {
		return "", fmt.Errorf("secret %s: %w", name, err)
	}
	return string(pt), nil
}

func parseIDs(s string) []int64 {
	out := []int64{}
	for _, p := range strings.Split(s, ",") {
		if n, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func granted(hosts []int64, id int64) bool {
	if len(hosts) == 0 {
		return true
	}
	for _, h := range hosts {
		if h == id {
			return true
		}
	}
	return false
}
