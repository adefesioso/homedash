package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/adefesioso/homedash/hub/internal/storage"
)

// storageTools list the pooled disks and the shared workspaces on them,
// so the hub's agent can point a job at a workspace by its path.
func storageTools(srv *sdk.Server, sto *storage.Storage) {
	addTool(srv, &sdk.Tool{
		Name:        "list_storage",
		Description: "Every storage cluster — its gateway, path, members and whether it is healthy or degraded — and every shared workspace on it: the path that appears on each named remote, and which of those remotes currently have it mounted. A workspace path is a valid working directory for start_job on any remote it is shared to.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, listStorageOut, error) {
		cs, err := sto.Statuses(ctx)
		if err != nil {
			return nil, listStorageOut{}, err
		}
		return nil, listStorageOut{Clusters: cs}, nil
	})
}

type listStorageOut struct {
	Clusters []storage.Status `json:"clusters"`
}
