package store

import (
	"context"
	"database/sql"
	"errors"
)

const clusterSchema = `
CREATE TABLE IF NOT EXISTS clusters (
	id         INTEGER PRIMARY KEY,
	name       TEXT NOT NULL UNIQUE,
	gateway_id INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
	path       TEXT NOT NULL,
	created    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE TABLE IF NOT EXISTS cluster_members (
	id         INTEGER PRIMARY KEY,
	cluster_id INTEGER NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
	host_id    INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
	path       TEXT NOT NULL,
	UNIQUE (host_id, path)
);
CREATE TABLE IF NOT EXISTS workspaces (
	id         INTEGER PRIMARY KEY,
	name       TEXT NOT NULL UNIQUE,
	cluster_id INTEGER NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
	created    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE TABLE IF NOT EXISTS workspace_members (
	workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
	host_id      INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
	PRIMARY KEY (workspace_id, host_id)
);
`

// Cluster is one filesystem at one path on its gateway, made of members.
type Cluster struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	GatewayID int64    `json:"gatewayId"`
	Gateway   string   `json:"gateway"`
	Path      string   `json:"path"`
	Members   []Member `json:"members"`
}

// Member is a host + path pair.
type Member struct {
	ID     int64  `json:"id"`
	HostID int64  `json:"hostId"`
	Host   string `json:"host"`
	Path   string `json:"path"`
}

// Clusters lists every cluster with its members.
func (s *Store) Clusters(ctx context.Context) ([]Cluster, error) {
	rows, err := s.RO.QueryContext(ctx,
		`SELECT c.id, c.name, c.gateway_id, h.name, c.path FROM clusters c JOIN hosts h ON h.id = c.gateway_id ORDER BY c.name`)
	if err != nil {
		return nil, err
	}
	out := []Cluster{}
	for rows.Next() {
		var c Cluster
		if err := rows.Scan(&c.ID, &c.Name, &c.GatewayID, &c.Gateway, &c.Path); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, c)
	}
	rows.Close()
	for i := range out {
		ms, err := s.members(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Members = ms
	}
	return out, nil
}

// ClusterByName resolves one.
func (s *Store) ClusterByName(ctx context.Context, name string) (*Cluster, error) {
	cs, err := s.Clusters(ctx)
	if err != nil {
		return nil, err
	}
	for i := range cs {
		if cs[i].Name == name {
			return &cs[i], nil
		}
	}
	return nil, errors.New("no such cluster")
}

// Cluster reads one by id.
func (s *Store) Cluster(ctx context.Context, id int64) (*Cluster, error) {
	var c Cluster
	err := s.RO.QueryRowContext(ctx,
		`SELECT c.id, c.name, c.gateway_id, h.name, c.path FROM clusters c JOIN hosts h ON h.id = c.gateway_id WHERE c.id = ?`, id).
		Scan(&c.ID, &c.Name, &c.GatewayID, &c.Gateway, &c.Path)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("no such cluster")
	}
	if err != nil {
		return nil, err
	}
	c.Members, err = s.members(ctx, id)
	return &c, err
}

func (s *Store) members(ctx context.Context, clusterID int64) ([]Member, error) {
	rows, err := s.RO.QueryContext(ctx,
		`SELECT m.id, m.host_id, h.name, m.path FROM cluster_members m JOIN hosts h ON h.id = m.host_id WHERE m.cluster_id = ? ORDER BY m.id`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.HostID, &m.Host, &m.Path); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CreateCluster records a cluster and its first members.
func (s *Store) CreateCluster(ctx context.Context, name string, gatewayID int64, path string, members []Member) (int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO clusters(name, gateway_id, path) VALUES (?, ?, ?)`, name, gatewayID, path)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	for _, m := range members {
		if _, err := tx.ExecContext(ctx, `INSERT INTO cluster_members(cluster_id, host_id, path) VALUES (?, ?, ?)`, id, m.HostID, m.Path); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

// AddMember records one more member.
func (s *Store) AddMember(ctx context.Context, clusterID, hostID int64, path string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO cluster_members(cluster_id, host_id, path) VALUES (?, ?, ?)`, clusterID, hostID, path)
	return err
}

// RemoveMember forgets one; its files stay where they are.
func (s *Store) RemoveMember(ctx context.Context, memberID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM cluster_members WHERE id = ?`, memberID)
	return err
}

// DeleteCluster forgets a cluster.
func (s *Store) DeleteCluster(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM clusters WHERE id = ?`, id)
	return err
}

// Workspace is a directory on a cluster shared to named remotes, at the
// same path on each.
type Workspace struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	ClusterID int64             `json:"clusterId"`
	Cluster   string            `json:"cluster"`
	Path      string            `json:"path"`
	Members   []WorkspaceMember `json:"members"`
}

// WorkspaceMember is one remote the workspace appears on.
type WorkspaceMember struct {
	HostID int64  `json:"hostId"`
	Host   string `json:"host"`
}

const workspaceCols = `w.id, w.name, w.cluster_id, c.name, c.path || '/' || w.name`

// Workspaces lists every workspace, optionally one cluster's, with members.
func (s *Store) Workspaces(ctx context.Context, clusterID int64) ([]Workspace, error) {
	q := `SELECT ` + workspaceCols + ` FROM workspaces w JOIN clusters c ON c.id = w.cluster_id`
	var args []any
	if clusterID != 0 {
		q += ` WHERE w.cluster_id = ?`
		args = append(args, clusterID)
	}
	rows, err := s.RO.QueryContext(ctx, q+` ORDER BY w.name`, args...)
	if err != nil {
		return nil, err
	}
	out := []Workspace{}
	for rows.Next() {
		var w Workspace
		if err := rows.Scan(&w.ID, &w.Name, &w.ClusterID, &w.Cluster, &w.Path); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, w)
	}
	rows.Close()
	for i := range out {
		ms, err := s.workspaceMembers(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Members = ms
	}
	return out, nil
}

// Workspace reads one by id.
func (s *Store) Workspace(ctx context.Context, id int64) (*Workspace, error) {
	var w Workspace
	err := s.RO.QueryRowContext(ctx, `SELECT `+workspaceCols+` FROM workspaces w JOIN clusters c ON c.id = w.cluster_id WHERE w.id = ?`, id).
		Scan(&w.ID, &w.Name, &w.ClusterID, &w.Cluster, &w.Path)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("no such workspace")
	}
	if err != nil {
		return nil, err
	}
	w.Members, err = s.workspaceMembers(ctx, id)
	return &w, err
}

func (s *Store) workspaceMembers(ctx context.Context, id int64) ([]WorkspaceMember, error) {
	rows, err := s.RO.QueryContext(ctx,
		`SELECT m.host_id, h.name FROM workspace_members m JOIN hosts h ON h.id = m.host_id WHERE m.workspace_id = ? ORDER BY h.name`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkspaceMember{}
	for rows.Next() {
		var m WorkspaceMember
		if err := rows.Scan(&m.HostID, &m.Host); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CreateWorkspace records a workspace and its first members.
func (s *Store) CreateWorkspace(ctx context.Context, name string, clusterID int64, hostIDs []int64) (int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO workspaces(name, cluster_id) VALUES (?, ?)`, name, clusterID)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	for _, h := range hostIDs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO workspace_members(workspace_id, host_id) VALUES (?, ?)`, id, h); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

// AddWorkspaceMember names one more remote.
func (s *Store) AddWorkspaceMember(ctx context.Context, id, hostID int64) error {
	_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO workspace_members(workspace_id, host_id) VALUES (?, ?)`, id, hostID)
	return err
}

// RemoveWorkspaceMember closes the share to one remote; the files stay.
func (s *Store) RemoveWorkspaceMember(ctx context.Context, id, hostID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM workspace_members WHERE workspace_id = ? AND host_id = ?`, id, hostID)
	return err
}

// DeleteWorkspace forgets a workspace; the directory stays on the cluster.
func (s *Store) DeleteWorkspace(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM workspaces WHERE id = ?`, id)
	return err
}
