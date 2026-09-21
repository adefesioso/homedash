// Package storage is the pooled disks and the shared workspaces on
// them: a cluster is a set of mountpoints
// on enrolled remotes presented as one filesystem at one path on a
// gateway, with their capacities added. mergerfs over NFS: a new file
// lands on the member with the most free space, a file is never split,
// an offline member is a hole, and a dead member errors instead of
// hanging (soft mounts). The hub sets the mount up and leaves the data
// path. This is the one place remotes trust each other: each member is
// exported to exactly one address, the gateway's.
package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/adefesioso/homedash/hub/internal/fleet"
	"github.com/adefesioso/homedash/hub/internal/gate"
	"github.com/adefesioso/homedash/hub/internal/store"
)

// Storage owns the clusters.
type Storage struct {
	Store  *store.Store
	Fleet  *fleet.Fleet
	Notify fleet.Notify
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// branchDir is where a member appears on the gateway before merging.
func branchDir(cluster string, m store.Member) string {
	return fmt.Sprintf("/mnt/homedash/%s/%s-%d", cluster, m.Host, m.ID)
}

// Status is what the Storage tab shows per cluster: capacity, and which
// members are reachable from the gateway right now.
type Status struct {
	Cluster  store.Cluster  `json:"cluster"`
	Mounted  bool           `json:"mounted"`
	Degraded bool           `json:"degraded"`
	Size     int64          `json:"size"`
	Free     int64          `json:"free"`
	Members  []MemberStatus `json:"members"`
	Error    string         `json:"error,omitempty"`
	// Workspaces are the shared directories on this cluster.
	Workspaces []WorkspaceStatus `json:"workspaces"`
}

// MemberStatus is one branch as the gateway sees it.
type MemberStatus struct {
	store.Member
	Reachable bool  `json:"reachable"`
	Size      int64 `json:"size"`
	Free      int64 `json:"free"`
}

// refuse is the gate for members: a path that carries the system, swap,
// or the cluster's own mount is never pooled.
func (s *Storage) refuse(ctx context.Context, h *store.Host, path string, clusterPath string) error {
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
		return errors.New("a member path is absolute")
	}
	for _, p := range []string{"/", "/boot", "/etc", "/usr", "/var", "/home", "/root"} {
		if path == p {
			return &gate.Refusal{Reason: "pooling a system path: " + h.Name + ":" + path}
		}
	}
	if path == clusterPath || strings.HasPrefix(path, "/mnt/homedash/") {
		return &gate.Refusal{Reason: "pooling a cluster's own mount: " + h.Name + ":" + path}
	}
	var f fleet.Facts
	_ = json.Unmarshal(h.Facts, &f)
	var raw struct {
		SystemSources []string `json:"systemSources"`
	}
	_ = json.Unmarshal(h.Facts, &raw)
	for _, m := range f.Mounts {
		if m.Path == path {
			for _, src := range raw.SystemSources {
				if src == m.Source {
					return &gate.Refusal{Reason: "pooling a disk that carries the system or swap: " + h.Name + ":" + path + " is on " + src}
				}
			}
			return nil
		}
	}
	return fmt.Errorf("%s has no mountpoint at %s; members are mountpoints the machine reported", h.Name, path)
}

// Create makes a cluster: exports on the members, mounts on the gateway.
func (s *Storage) Create(ctx context.Context, name string, gateway *store.Host, path string, members []store.Member) (*store.Cluster, error) {
	if !nameRe.MatchString(name) {
		return nil, errors.New("a cluster name is lowercase letters, digits, - and _")
	}
	if len(members) < 2 {
		return nil, errors.New("a cluster is two or more members")
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "/mnt/homedash/") {
		return nil, errors.New("the cluster path is absolute and not under /mnt/homedash")
	}
	for i := range members {
		h, err := s.Store.Host(ctx, strconv.FormatInt(members[i].HostID, 10))
		if err != nil {
			return nil, err
		}
		if err := s.refuse(ctx, h, members[i].Path, path); err != nil {
			s.Notify("gate.refused", h.Name, err.Error())
			return nil, err
		}
	}
	id, err := s.Store.CreateCluster(ctx, name, gateway.ID, path, members)
	if err != nil {
		return nil, err
	}
	c, err := s.Store.Cluster(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.apply(ctx, c); err != nil {
		_ = s.Store.DeleteCluster(ctx, id)
		return nil, err
	}
	s.Notify("cluster.created", name, fmt.Sprintf("cluster %s: %d members at %s on %s", name, len(members), path, gateway.Name))
	return c, nil
}

// AddMember grows a cluster; the mount is rebuilt with the new branch.
func (s *Storage) AddMember(ctx context.Context, c *store.Cluster, h *store.Host, path string) error {
	if err := s.refuse(ctx, h, path, c.Path); err != nil {
		s.Notify("gate.refused", h.Name, err.Error())
		return err
	}
	if err := s.Store.AddMember(ctx, c.ID, h.ID, path); err != nil {
		return err
	}
	c, err := s.Store.Cluster(ctx, c.ID)
	if err != nil {
		return err
	}
	return s.apply(ctx, c)
}

// RemoveMember rebuilds the mount without a member and drops its export;
// the member's files stay exactly where they are.
func (s *Storage) RemoveMember(ctx context.Context, c *store.Cluster, memberID int64) error {
	var gone *store.Member
	for i := range c.Members {
		if c.Members[i].ID == memberID {
			gone = &c.Members[i]
		}
	}
	if gone == nil {
		return errors.New("no such member")
	}
	if len(c.Members) <= 2 {
		return errors.New("a cluster keeps at least two members; delete the cluster instead")
	}
	if err := s.Store.RemoveMember(ctx, memberID); err != nil {
		return err
	}
	c, err := s.Store.Cluster(ctx, c.ID)
	if err != nil {
		return err
	}
	if err := s.apply(ctx, c); err != nil {
		return err
	}
	s.unexport(ctx, c, *gone)
	_ = s.runOnGateway(ctx, c, "umount -l "+shq(branchDir(c.Name, *gone))+" 2>/dev/null; rmdir "+shq(branchDir(c.Name, *gone))+" 2>/dev/null; sed -i '\\# "+branchDir(c.Name, *gone)+" #d' /etc/fstab")
	return nil
}

// Delete tears a cluster down. Every member goes on being an ordinary
// readable disk with the files already on it.
func (s *Storage) Delete(ctx context.Context, c *store.Cluster) error {
	_ = s.runOnGateway(ctx, c, "umount -l "+shq(c.Path)+" 2>/dev/null; for b in /mnt/homedash/"+c.Name+"/*; do umount -l \"$b\" 2>/dev/null; done; sed -i '\\#homedash-cluster-"+c.Name+"#d' /etc/fstab; rm -rf /mnt/homedash/"+c.Name)
	for _, m := range c.Members {
		s.unexport(ctx, c, m)
	}
	if err := s.Store.DeleteCluster(ctx, c.ID); err != nil {
		return err
	}
	s.Notify("cluster.deleted", c.Name, "cluster "+c.Name+" torn down; every member keeps its files")
	return nil
}

// apply is the whole mount, from the current member list: exports on
// the members, then branches and the merged mount on the gateway. It
// is idempotent, so a rebuild is the same call.
func (s *Storage) apply(ctx context.Context, c *store.Cluster) error {
	gw, err := s.Store.Host(ctx, strconv.FormatInt(c.GatewayID, 10))
	if err != nil {
		return err
	}
	var branches []string
	var fstab strings.Builder
	for _, m := range c.Members {
		if m.HostID == c.GatewayID {
			branches = append(branches, m.Path)
			continue
		}
		mh, err := s.Store.Host(ctx, strconv.FormatInt(m.HostID, 10))
		if err != nil {
			return err
		}
		if err := s.export(ctx, c, mh, m, gw.Addr); err != nil {
			return err
		}
		b := branchDir(c.Name, m)
		branches = append(branches, b)
		// Eager, not x-systemd.automount: the gateway also exports the
		// merge below through its own nfs-server, whose exportfs -r walks
		// that path on every start. x-systemd.automount makes any access
		// to an unmounted branch block the caller until the real mount
		// job finishes — and nfs-utils orders that job after
		// nfs-server.service on a host that is both client and server, so
		// a reboot deadlocks: exportfs blocked reading through the merge,
		// the merge blocked reading the branch, the branch's mount
		// waiting on nfs-server, nfs-server waiting on exportfs. A plain
		// deferred mount just reads as an empty directory in the meantime
		// (mergerfs already tolerates that — see the loop below), so
		// nothing blocks and the branch catches up once its mount job runs.
		fmt.Fprintf(&fstab, "%s:%s %s nfs soft,timeo=30,retrans=2,_netdev,nofail 0 0 # homedash-cluster-%s\n", mh.Addr, m.Path, b, c.Name)
	}
	fmt.Fprintf(&fstab, "%s %s fuse.mergerfs defaults,allow_other,category.create=mfs,moveonenospc=true,minfreespace=1G,nofail 0 0 # homedash-cluster-%s\n",
		strings.Join(branches, ":"), c.Path, c.Name)
	script := `set -e
export DEBIAN_FRONTEND=noninteractive
command -v mergerfs >/dev/null 2>&1 || apt-get -qq install -y mergerfs nfs-common >/dev/null
dpkg -s nfs-common >/dev/null 2>&1 || apt-get -qq install -y nfs-common >/dev/null
umount -l ` + shq(c.Path) + ` 2>/dev/null || true
sed -i '\#homedash-cluster-` + c.Name + `#d' /etc/fstab
mkdir -p ` + shq(c.Path) + ` /mnt/homedash/` + c.Name + `
cat >> /etc/fstab <<'HOMEDASH_FSTAB'
` + fstab.String() + `HOMEDASH_FSTAB
systemctl daemon-reload
for b in /mnt/homedash/` + c.Name + `/*; do [ -d "$b" ] || continue; mountpoint -q "$b" || timeout 20 mount "$b" || echo "member at $b did not mount (it reads as a hole until it does)" >&2; done
`
	for _, b := range branches {
		if strings.HasPrefix(b, "/mnt/homedash/") {
			script += "mkdir -p " + shq(b) + "; mountpoint -q " + shq(b) + " || timeout 20 mount " + shq(b) + " || true\n"
		}
	}
	// The cluster's root belongs to the hub's account, group-writable: a
	// job on the gateway is in that group and works the cluster directly.
	script += "mount " + shq(c.Path) + "\nmountpoint -q " + shq(c.Path) + "\nchown homedash:homedash " + shq(c.Path) + " && chmod 2775 " + shq(c.Path) + " || true\n"
	if err := s.runOnGateway(ctx, c, script); err != nil {
		return err
	}
	_ = s.Store.AppendRebuildScript(ctx, gw.ID, "# HomeDash: cluster "+c.Name+" gateway mount\n"+script)
	return nil
}

// export makes a member's path reachable from the gateway and nowhere
// else. Done through NFS so the gateway's kernel does the reads.
func (s *Storage) export(ctx context.Context, c *store.Cluster, mh *store.Host, m store.Member, gatewayAddr string) error {
	script := `set -e
export DEBIAN_FRONTEND=noninteractive
dpkg -s nfs-kernel-server >/dev/null 2>&1 || apt-get -qq install -y nfs-kernel-server >/dev/null
mkdir -p /etc/exports.d
printf '%s %s(rw,sync,no_subtree_check,no_root_squash)\n' ` + shq(m.Path) + ` ` + shq(gatewayAddr) + ` > /etc/exports.d/homedash-` + c.Name + `-` + strconv.FormatInt(m.ID, 10) + `.exports
systemctl enable --now nfs-server >/dev/null 2>&1 || true
exportfs -ra
`
	r, err := s.Fleet.Exec.Run(ctx, fleet.Target(mh), []string{"sudo", "-n", "sh", "-s"}, strings.NewReader(script))
	if err != nil {
		return fmt.Errorf("%s: %w", mh.Name, err)
	}
	if r.ExitCode != 0 {
		return fmt.Errorf("export %s:%s: %s", mh.Name, m.Path, lastLine(r.Stderr))
	}
	_ = s.Store.AppendRebuildScript(ctx, mh.ID, "# HomeDash: cluster "+c.Name+" export of "+m.Path+" to "+gatewayAddr+"\n"+script)
	return nil
}

func (s *Storage) unexport(ctx context.Context, c *store.Cluster, m store.Member) {
	mh, err := s.Store.Host(ctx, strconv.FormatInt(m.HostID, 10))
	if err != nil || m.HostID == c.GatewayID {
		return
	}
	_, _ = s.Fleet.Exec.Run(ctx, fleet.Target(mh), []string{"sudo", "-n", "sh", "-c",
		"rm -f /etc/exports.d/homedash-" + c.Name + "-" + strconv.FormatInt(m.ID, 10) + ".exports; exportfs -ra"}, nil)
}

func (s *Storage) runOnGateway(ctx context.Context, c *store.Cluster, script string) error {
	gw, err := s.Store.Host(ctx, strconv.FormatInt(c.GatewayID, 10))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	r, err := s.Fleet.Exec.Run(ctx, fleet.Target(gw), []string{"sudo", "-n", "sh", "-s"}, strings.NewReader(script))
	if err != nil {
		return fmt.Errorf("%s: %w", gw.Name, err)
	}
	if r.ExitCode != 0 {
		return fmt.Errorf("on %s: %s", gw.Name, lastLine(r.Stderr))
	}
	return nil
}

// Statuses asks each gateway what its clusters look like right now. A
// Degraded cluster names the member; the transition is an event.
func (s *Storage) Statuses(ctx context.Context) ([]Status, error) {
	cs, err := s.Store.Clusters(ctx)
	if err != nil {
		return nil, err
	}
	out := []Status{}
	for _, c := range cs {
		st := Status{Cluster: c, Members: []MemberStatus{}}
		st.Workspaces, _ = s.WorkspaceStatuses(ctx, c.ID)
		gw, err := s.Store.Host(ctx, strconv.FormatInt(c.GatewayID, 10))
		if err != nil || gw.Status != "online" {
			st.Error = "gateway is not online"
			out = append(out, st)
			continue
		}
		var script strings.Builder
		script.WriteString("mountpoint -q " + shq(c.Path) + " && df -B1 --output=size,avail " + shq(c.Path) + " | tail -1 || echo 'x x'\n")
		for _, m := range c.Members {
			b := m.Path
			if m.HostID != c.GatewayID {
				b = branchDir(c.Name, m)
			}
			script.WriteString("(timeout 5 stat " + shq(b) + " >/dev/null 2>&1 && (mountpoint -q " + shq(b) + " || [ " + shq(b) + " = " + shq(m.Path) + " ]) && df -B1 --output=size,avail " + shq(b) + " | tail -1) || echo 'x x'\n")
		}
		r, err := s.Fleet.Exec.Run(ctx, fleet.Target(gw), []string{"sh", "-s"}, strings.NewReader(script.String()))
		if err != nil {
			st.Error = err.Error()
			out = append(out, st)
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(r.Stdout)), "\n")
		if len(lines) > 0 {
			st.Size, st.Free, st.Mounted = parseDF(lines[0])
		}
		for i, m := range c.Members {
			ms := MemberStatus{Member: m}
			if i+1 < len(lines) {
				ms.Size, ms.Free, ms.Reachable = parseDF(lines[i+1])
			}
			if !ms.Reachable {
				st.Degraded = true
			}
			st.Members = append(st.Members, ms)
		}
		out = append(out, st)
	}
	return out, nil
}

func parseDF(line string) (size, free int64, ok bool) {
	f := strings.Fields(line)
	if len(f) < 2 {
		return 0, 0, false
	}
	size, err1 := strconv.ParseInt(f[0], 10, 64)
	free, err2 := strconv.ParseInt(f[1], 10, 64)
	return size, free, err1 == nil && err2 == nil
}

// Gateway resolves a cluster name to its gateway host, for placement.
func (s *Storage) Gateway(ctx context.Context, name string) (*store.Host, error) {
	c, err := s.Store.ClusterByName(ctx, name)
	if err != nil {
		return nil, err
	}
	return s.Store.Host(ctx, strconv.FormatInt(c.GatewayID, 10))
}

func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func lastLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	return s
}
