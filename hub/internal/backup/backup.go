// Package backup is how state survives the hub: the whole state
// directory — the database, the hub's key, the secrets key, the vault and
// the session files — exported as one file encrypted under a passphrase,
// and restored from that file into a hub that has nothing open.
package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"

	"github.com/adefesioso/homedash/hub/internal/store"
)

// Backup exports the state directory.
type Backup struct {
	Store    *store.Store
	StateDir string
}

// Status is what Settings shows.
type Status struct {
	Last string `json:"last,omitempty"`
}

func (b *Backup) Status(ctx context.Context) Status {
	last, _ := b.Store.Setting(ctx, "backup.last")
	return Status{Last: last}
}

// pendingDir is where a staged restore waits for a start that applies it.
const pendingDir = "restore.pending"

// The loose files an export carries; everything else is under omp/.
var stateFiles = []string{"hub_key", "hub_key.pub", "secrets.key", "peer_key"}

// Export writes the state directory to w as an age-encrypted tar: a
// consistent copy of the database, the keys and omp's own files. The
// time is recorded as backup.last on success.
func (b *Backup) Export(ctx context.Context, w io.Writer, passphrase string) error {
	if passphrase == "" {
		return errors.New("an export needs a passphrase")
	}
	tmp, err := os.MkdirTemp(b.StateDir, ".export-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if _, err := b.Store.DB.ExecContext(ctx, "VACUUM INTO ?", filepath.Join(tmp, "homedash.db")); err != nil {
		return fmt.Errorf("copy database: %w", err)
	}
	// Built in memory first so a refused write never leaves w half done.
	var buf bytes.Buffer
	if err := pack(&buf, b.StateDir, filepath.Join(tmp, "homedash.db"), passphrase); err != nil {
		return err
	}
	if _, err := io.Copy(w, &buf); err != nil {
		return err
	}
	_ = b.Store.SetSetting(ctx, "backup.last", time.Now().UTC().Format(time.RFC3339))
	return nil
}

// FileName is the download's name: the moment it was made, in UTC.
func FileName() string { return "homedash-" + time.Now().UTC().Format("20060102-150405") + ".tar.age" }

func pack(w io.Writer, stateDir, dbCopy, passphrase string) error {
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return err
	}
	enc, err := age.Encrypt(w, recipient)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(enc)
	add := func(name, src string) error {
		st, err := os.Stat(src)
		if err != nil {
			return err
		}
		f, err := os.Open(src)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: int64(st.Mode().Perm()), Size: st.Size(), ModTime: st.ModTime()}); err != nil {
			return err
		}
		_, err = io.Copy(tw, f)
		return err
	}
	if err := add("homedash.db", dbCopy); err != nil {
		return err
	}
	for _, f := range stateFiles {
		if err := add(f, filepath.Join(stateDir, f)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	// omp's own files: the vault and the sessions, not the binary, not caches.
	ompRoot := filepath.Join(stateDir, "omp")
	err = filepath.WalkDir(ompRoot, func(p string, d os.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			if d != nil && d.IsDir() {
				switch d.Name() {
				case "bin", "windows", "cache", "logs":
					return filepath.SkipDir
				}
			}
			return nil
		}
		rel, _ := filepath.Rel(stateDir, p)
		if strings.HasSuffix(rel, "-wal") || strings.HasSuffix(rel, "-shm") || strings.HasSuffix(rel, ".db") && d.Name() != "agent.db" {
			return nil
		}
		return add(filepath.ToSlash(rel), p)
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return enc.Close()
}

// Stage opens an export with the passphrase and lays its files out in
// <state>/restore.pending, touching nothing live: a wrong passphrase or
// a file that is not an export is refused with no damage. Only the names
// an export writes are accepted, and the database must be a SQLite file.
func Stage(stateDir string, r io.Reader, passphrase string) (err error) {
	if passphrase == "" {
		return errors.New("a restore needs the passphrase the export was made with")
	}
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return err
	}
	dec, err := age.Decrypt(r, id)
	if err != nil {
		if errors.Is(err, age.ErrIncorrectIdentity) {
			return errors.New("that passphrase does not open this file")
		}
		return fmt.Errorf("not a HomeDash export: %w", err)
	}
	pending := filepath.Join(stateDir, pendingDir)
	_ = os.RemoveAll(pending)
	if err := os.MkdirAll(pending, 0o700); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(pending)
		}
	}()
	tr := tar.NewReader(dec)
	hasDB := false
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if errors.Is(err, age.ErrIncorrectIdentity) {
				return errors.New("that passphrase does not open this file")
			}
			return fmt.Errorf("not a HomeDash export: %w", err)
		}
		if h.Typeflag != tar.TypeReg {
			return fmt.Errorf("not a HomeDash export: %s is not a file", h.Name)
		}
		name := path.Clean(h.Name)
		if !allowed(name) {
			return fmt.Errorf("not a HomeDash export: unexpected entry %s", h.Name)
		}
		dst := filepath.Join(pending, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode&0o777)|0o600)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, tr)
		f.Close()
		if err != nil {
			return err
		}
		if name == "homedash.db" {
			head := make([]byte, 16)
			g, _ := os.Open(dst)
			n, _ := io.ReadFull(g, head)
			g.Close()
			if n < 16 || string(head[:15]) != "SQLite format 3" {
				return errors.New("not a HomeDash export: the database inside is not SQLite")
			}
			hasDB = true
		}
	}
	if !hasDB {
		return errors.New("not a HomeDash export: no database inside")
	}
	return nil
}

func allowed(name string) bool {
	if name == "homedash.db" {
		return true
	}
	for _, f := range stateFiles {
		if name == f {
			return true
		}
	}
	return strings.HasPrefix(name, "omp/") && !strings.Contains(name, "..") && !path.IsAbs(name)
}

// Pending says whether a staged restore is waiting.
func Pending(stateDir string) bool {
	st, err := os.Stat(filepath.Join(stateDir, pendingDir))
	return err == nil && st.IsDir()
}

// Apply moves a staged restore over the live state and removes the
// staging directory. Only for a state directory nothing has open: the
// database's -wal/-shm sidecars are dropped so SQLite reads the restored
// file, not a stale journal. Returns false when nothing was staged.
func Apply(stateDir string) (bool, error) {
	pending := filepath.Join(stateDir, pendingDir)
	if !Pending(stateDir) {
		return false, nil
	}
	err := filepath.WalkDir(pending, func(p string, d os.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return werr
		}
		rel, _ := filepath.Rel(pending, p)
		dst := filepath.Join(stateDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		if strings.HasSuffix(rel, ".db") {
			_ = os.Remove(dst + "-wal")
			_ = os.Remove(dst + "-shm")
		}
		return os.Rename(p, dst)
	})
	if err != nil {
		return false, fmt.Errorf("apply restore: %w", err)
	}
	return true, os.RemoveAll(pending)
}
