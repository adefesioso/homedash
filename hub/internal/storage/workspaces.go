package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/adefesioso/homedash/hub/internal/fleet"
	"github.com/adefesioso/homedash/hub/internal/store"
)

// A shared workspace is a directory on a cluster, shared by the gateway
// to exactly the remotes named, at the same path on each. The cluster's
// rules carry over unchanged; what changes is that several remotes'
// agents work the same files without the hub's agent relaying them.

// WorkspaceStatus is one workspace as the Storage tab shows it: which
// named remotes actually have it mounted, by their own word.
type WorkspaceStatus struct {
	store.Workspace
	Mounted map[string]bool `json:"mounted"` // host name → its facts list the path
}

// exportsFile is the gateway's export of a workspace to its remotes.
func exportsFile(name string) string { return "/etc/exports.d/homedash-ws-" + name + ".exports" }

// fsid is what the kernel NFS server needs to export a FUSE path: a
// stable id, derived from the name so a rebuild gets the same one.
func fsid(name string) string {
	sum := sha256.Sum256([]byte("homedash-ws/" + name))
	h := hex.EncodeToString(sum[:16])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// CreateWorkspace makes the directory on the cluster and shares it to
// the named remotes.
func (s *Storage) CreateWorkspace(ctx context.Context, c *store.Cluster, name string, hosts []*store.Host) (*store.Workspace, error) {
	if !nameRe.MatchString(name) {
		return nil, errors.New("a workspace name is lowercase letters, digits, - and _")
	}
	if len(hosts) == 0 {
		return nil, errors.New("a workspace names at least one remote")
	}
	ids := make([]int64, 0, len(hosts))
	for _, h := range hosts {
		ids = append(ids, h.ID)
	}
	id, err := s.Store.CreateWorkspace(ctx, name, c.ID, ids)
	if err != nil {
		return nil, err
	}
	w, err := s.Store.Workspace(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.share(ctx, w); err != nil {
		_ = s.Store.DeleteWorkspace(ctx, id)
		return nil, err
	}
	s.Notify("workspace.created", name, fmt.Sprintf("workspace %s at %s shared to %d remote(s)", name, w.Path, len(hosts)))
	return w, nil
}

// AddWorkspaceMember names one more remote and mounts it there.
func (s *Storage) AddWorkspaceMember(ctx context.Context, w *store.Workspace, h *store.Host) error {
	if err := s.Store.AddWorkspaceMember(ctx, w.ID, h.ID); err != nil {
		return err
	}
	w, err := s.Store.Workspace(ctx, w.ID)
	if err != nil {
		return err
	}
	return s.share(ctx, w)
}

// RemoveWorkspaceMember closes the share to one remote; the files stay.
func (s *Storage) RemoveWorkspaceMember(ctx context.Context, w *store.Workspace, h *store.Host) error {
	if err := s.Store.RemoveWorkspaceMember(ctx, w.ID, h.ID); err != nil {
		return err
	}
	s.unmountWorkspace(ctx, w, h)
	w, err := s.Store.Workspace(ctx, w.ID)
	if err != nil {
		return err
	}
	return s.share(ctx, w)
}

// DeleteWorkspace unshares from every remote and leaves the directory
// and its files on the cluster.
func (s *Storage) DeleteWorkspace(ctx context.Context, w *store.Workspace) error {
	c, err := s.Store.Cluster(ctx, w.ClusterID)
	if err != nil {
		return err
	}
	for _, m := range w.Members {
		if h, err := s.Store.Host(ctx, strconv.FormatInt(m.HostID, 10)); err == nil {
			s.unmountWorkspace(ctx, w, h)
		}
	}
	_ = s.runOnGateway(ctx, c, "rm -f "+shq(exportsFile(w.Name))+"; exportfs -ra")
	if err := s.Store.DeleteWorkspace(ctx, w.ID); err != nil {
		return err
	}
	s.Notify("workspace.deleted", w.Name, "workspace "+w.Name+" unshared; its files stay at "+w.Path)
	return nil
}

// share is the whole share from the current member list: the export on
// the gateway to exactly the named remotes, and the mount on each remote
// that is not the gateway. Idempotent.
func (s *Storage) share(ctx context.Context, w *store.Workspace) error {
	c, err := s.Store.Cluster(ctx, w.ClusterID)
	if err != nil {
		return err
	}
	gw, err := s.Store.Host(ctx, strconv.FormatInt(c.GatewayID, 10))
	if err != nil {
		return err
	}
	var clients []string
	var remotes []*store.Host
	for _, m := range w.Members {
		if m.HostID == c.GatewayID {
			continue
		}
		h, err := s.Store.Host(ctx, strconv.FormatInt(m.HostID, 10))
		if err != nil {
			return err
		}
		remotes = append(remotes, h)
		clients = append(clients, fmt.Sprintf("%s(rw,sync,no_subtree_check,all_squash,anonuid=$HD_UID,anongid=$HD_GID,fsid=%s)", h.Addr, fsid(w.Name)))
	}
	// The export: every remote's writes land as the gateway's own account,
	// so the files have one owner however many machines touch them.
	script := `set -e
export DEBIAN_FRONTEND=noninteractive
dpkg -s nfs-kernel-server >/dev/null 2>&1 || apt-get -qq install -y nfs-kernel-server >/dev/null
HD_UID=$(id -u homedash); HD_GID=$(id -g homedash)
mkdir -p ` + shq(w.Path) + `
chown homedash:homedash ` + shq(w.Path) + `
chmod 2775 ` + shq(w.Path) + `
mkdir -p /etc/exports.d
`
	if len(clients) == 0 {
		script += "rm -f " + shq(exportsFile(w.Name)) + "\n"
	} else {
		script += "printf '%s %s\\n' " + shq(w.Path) + " \"" + strings.Join(clients, " ") + "\" > " + shq(exportsFile(w.Name)) + "\n"
	}
	script += "systemctl enable --now nfs-server >/dev/null 2>&1 || true\nexportfs -ra\n"
	if err := s.runOnGateway(ctx, c, script); err != nil {
		return err
	}
	_ = s.Store.AppendRebuildScript(ctx, gw.ID, "# HomeDash: workspace "+w.Name+" shared from the gateway\n"+script)
	for _, h := range remotes {
		if err := s.mountWorkspace(ctx, w, h, gw.Addr); err != nil {
			return err
		}
	}
	return nil
}

// mountWorkspace puts the share at the same path on one remote.
func (s *Storage) mountWorkspace(ctx context.Context, w *store.Workspace, h *store.Host, gatewayAddr string) error {
	tag := "homedash-workspace-" + w.Name
	script := `set -e
export DEBIAN_FRONTEND=noninteractive
dpkg -s nfs-common >/dev/null 2>&1 || apt-get -qq install -y nfs-common >/dev/null
umount -l ` + shq(w.Path) + ` 2>/dev/null || true
sed -i '\#` + tag + `#d' /etc/fstab
mkdir -p ` + shq(w.Path) + `
printf '%s:%s %s nfs soft,timeo=30,retrans=2,_netdev,nofail,x-systemd.automount 0 0 # ` + tag + `\n' ` + shq(gatewayAddr) + ` ` + shq(w.Path) + ` ` + shq(w.Path) + ` >> /etc/fstab
systemctl daemon-reload
timeout 20 mount ` + shq(w.Path) + ` || echo "` + w.Path + ` did not mount yet (it reads as absent until it does)" >&2
`
	r, err := s.Fleet.Exec.Run(ctx, fleet.Target(h), []string{"sudo", "-n", "sh", "-s"}, strings.NewReader(script))
	if err != nil {
		return fmt.Errorf("%s: %w", h.Name, err)
	}
	if r.ExitCode != 0 {
		return fmt.Errorf("mount %s on %s: %s", w.Path, h.Name, lastLine(r.Stderr))
	}
	_ = s.Store.AppendRebuildScript(ctx, h.ID, "# HomeDash: workspace "+w.Name+" mounted from "+gatewayAddr+"\n"+script)
	return nil
}

func (s *Storage) unmountWorkspace(ctx context.Context, w *store.Workspace, h *store.Host) {
	c, err := s.Store.Cluster(ctx, w.ClusterID)
	if err != nil || h.ID == c.GatewayID {
		return
	}
	tag := "homedash-workspace-" + w.Name
	_, _ = s.Fleet.Exec.Run(ctx, fleet.Target(h), []string{"sudo", "-n", "sh", "-c",
		"umount -l " + shq(w.Path) + " 2>/dev/null; sed -i '\\#" + tag + "#d' /etc/fstab; systemctl daemon-reload; rmdir " + shq(w.Path) + " 2>/dev/null || true"}, nil)
}

// WorkspaceStatuses lists every workspace with what each named remote's
// own facts say about the mount.
func (s *Storage) WorkspaceStatuses(ctx context.Context, clusterID int64) ([]WorkspaceStatus, error) {
	ws, err := s.Store.Workspaces(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	out := []WorkspaceStatus{}
	for _, w := range ws {
		st := WorkspaceStatus{Workspace: w, Mounted: map[string]bool{}}
		for _, m := range w.Members {
			h, err := s.Store.Host(ctx, strconv.FormatInt(m.HostID, 10))
			if err != nil {
				continue
			}
			if c, err := s.Store.Cluster(ctx, w.ClusterID); err == nil && h.ID == c.GatewayID {
				st.Mounted[h.Name] = h.Status == "online"
				continue
			}
			var f fleet.Facts
			_ = json.Unmarshal(h.Facts, &f)
			for _, mt := range f.Mounts {
				if mt.Path == w.Path {
					st.Mounted[h.Name] = true
				}
			}
		}
		out = append(out, st)
	}
	return out, nil
}
