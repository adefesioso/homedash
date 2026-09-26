package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/adefesioso/homedash/hub/internal/storage"
	"github.com/adefesioso/homedash/hub/internal/store"
)

// storageTools are how the hub's agent coordinates remotes that need to
// hand each other files: it pools the disks jobs formatted and shares
// workspaces on them. A job never mounts another remote itself; every
// export here is the hub's, to the addresses it names.
func storageTools(srv *sdk.Server, st *store.Store, sto *storage.Storage) {
	addTool(srv, &sdk.Tool{
		Name:        "list_storage",
		Description: "Every storage cluster — its gateway, path, members and whether it is healthy or degraded — and every shared workspace on it: its id, the path that appears on each named remote, and which of those remotes currently have it mounted. A workspace path is a valid working directory for start_job on any remote it is shared to.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, listStorageOut, error) {
		cs, err := sto.Statuses(ctx)
		if err != nil {
			return nil, listStorageOut{}, err
		}
		return nil, listStorageOut{Clusters: cs}, nil
	})

	addTool(srv, &sdk.Tool{
		Name:        "cluster_create",
		Description: "Pool mountpoints on remotes into one filesystem at one path on a gateway remote. Each member is a host and a path already mounted there — a data disk a job formatted and mounted. The system disk, swap and the cluster's own mount are refused.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: ptr(true)},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in clusterIn) (*sdk.CallToolResult, clusterOut, error) {
		gw, err := st.Host(ctx, in.Gateway)
		if err != nil {
			return nil, clusterOut{}, err
		}
		var members []store.Member
		for _, m := range in.Members {
			h, err := st.Host(ctx, m.Host)
			if err != nil {
				return nil, clusterOut{}, err
			}
			members = append(members, store.Member{HostID: h.ID, Host: h.Name, Path: m.Path})
		}
		c, err := sto.Create(ctx, in.Name, gw, in.Path, members)
		if err != nil {
			return nil, clusterOut{}, err
		}
		return nil, clusterOut{Cluster: *c}, nil
	})

	addTool(srv, &sdk.Tool{
		Name:        "cluster_add_member",
		Description: "Add a host's mounted path to an existing cluster, by the cluster's name.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: ptr(true)},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in clusterMemberIn) (*sdk.CallToolResult, okOut, error) {
		c, err := st.ClusterByName(ctx, in.Cluster)
		if err != nil {
			return nil, okOut{}, err
		}
		h, err := st.Host(ctx, in.Host)
		if err != nil {
			return nil, okOut{}, err
		}
		if err := sto.AddMember(ctx, c, h, in.Path); err != nil {
			return nil, okOut{}, err
		}
		return nil, okOut{OK: true}, nil
	})

	addTool(srv, &sdk.Tool{
		Name:        "workspace_create",
		Description: "Make a shared workspace: a directory on a cluster that appears at the same path on every remote named, so jobs on those remotes hand each other notes, data and runnables as files. This is how two remotes work together; they never reach each other directly.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: ptr(false)},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in workspaceIn) (*sdk.CallToolResult, workspaceOut, error) {
		c, err := st.ClusterByName(ctx, in.Cluster)
		if err != nil {
			return nil, workspaceOut{}, err
		}
		var hosts []*store.Host
		for _, ref := range in.Hosts {
			h, err := st.Host(ctx, ref)
			if err != nil {
				return nil, workspaceOut{}, err
			}
			hosts = append(hosts, h)
		}
		ws, err := sto.CreateWorkspace(ctx, c, in.Name, hosts)
		if err != nil {
			return nil, workspaceOut{}, err
		}
		return nil, workspaceOut{Workspace: *ws}, nil
	})

	addTool(srv, &sdk.Tool{
		Name:        "workspace_member",
		Description: "Share a workspace (by id, from list_storage) with one more remote, or stop sharing it (remove: true). Removing closes that remote's share; the files stay.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: ptr(true)},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in workspaceMemberIn) (*sdk.CallToolResult, okOut, error) {
		ws, err := st.Workspace(ctx, in.ID)
		if err != nil {
			return nil, okOut{}, err
		}
		h, err := st.Host(ctx, in.Host)
		if err != nil {
			return nil, okOut{}, err
		}
		if in.Remove {
			err = sto.RemoveWorkspaceMember(ctx, ws, h)
		} else {
			err = sto.AddWorkspaceMember(ctx, ws, h)
		}
		if err != nil {
			return nil, okOut{}, err
		}
		return nil, okOut{OK: true}, nil
	})
}

type listStorageOut struct {
	Clusters []storage.Status `json:"clusters"`
}
type clusterIn struct {
	Name    string `json:"name"`
	Gateway string `json:"gateway" jsonschema:"the host the pooled path appears on"`
	Path    string `json:"path" jsonschema:"where the pooled filesystem appears on the gateway"`
	Members []struct {
		Host string `json:"host"`
		Path string `json:"path" jsonschema:"a path already mounted on that host"`
	} `json:"members"`
}
type clusterOut struct {
	Cluster store.Cluster `json:"cluster"`
}
type clusterMemberIn struct {
	Cluster string `json:"cluster" jsonschema:"the cluster's name"`
	Host    string `json:"host"`
	Path    string `json:"path"`
}
type workspaceIn struct {
	Cluster string   `json:"cluster" jsonschema:"the cluster's name"`
	Name    string   `json:"name"`
	Hosts   []string `json:"hosts" jsonschema:"the remotes it appears on"`
}
type workspaceOut struct {
	Workspace store.Workspace `json:"workspace"`
}
type workspaceMemberIn struct {
	ID     int64  `json:"id" jsonschema:"the workspace's id"`
	Host   string `json:"host"`
	Remove bool   `json:"remove,omitempty"`
}
