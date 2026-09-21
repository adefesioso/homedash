package store

import (
	"context"
	"testing"
)

func TestReadOnlyPoolRefusesWrites(t *testing.T) {
	st, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.RO.Exec(`INSERT INTO settings(key, value) VALUES ('x','y')`); err == nil {
		t.Fatal("read-only pool accepted a write")
	}
	var sync string
	if err := st.RO.QueryRow(`PRAGMA synchronous`).Scan(&sync); err != nil || sync != "1" {
		t.Fatalf("synchronous = %q err=%v, want 1 (NORMAL)", sync, err)
	}
	if err := st.DB.QueryRow(`PRAGMA synchronous`).Scan(&sync); err != nil || sync != "1" {
		t.Fatalf("writer synchronous = %q err=%v", sync, err)
	}
}
