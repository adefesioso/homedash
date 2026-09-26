package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/adefesioso/homedash/hub/internal/fleet"
	"github.com/adefesioso/homedash/hub/internal/store"
)

// hostTools are the tools that name a host. Each resolves the name through
// the store first, so an unknown host — and "the hub", which is not a host
// — is an error before anything else happens.
func hostTools(srv *sdk.Server, st *store.Store, fl *fleet.Fleet) {
	addTool(srv, &sdk.Tool{
		Name:        "list_hosts",
		Description: "Every enrolled remote: name, status (online, offline, mismatch, unknown), what it last reported about itself (cores, memory, mounts, GPU, Docker, Ollama, the lock, its agent's version and model), and when it was last seen. The hub itself is not a host and never appears here.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, hostsOut, error) {
		hs, err := st.Hosts(ctx)
		if err != nil {
			return nil, hostsOut{}, err
		}
		out := hostsOut{Hosts: make([]hostRow, 0, len(hs))}
		for _, h := range hs {
			out.Hosts = append(out.Hosts, hostRow{ID: h.ID, Name: h.Name, Addr: h.Addr, Status: h.Status, Facts: h.Facts, LastSeen: h.LastSeen})
		}
		return nil, out, nil
	})

	addTool(srv, &sdk.Tool{
		Name:        "lock_host",
		Description: "Turn direct SSH access on a host off (locked: only the hub's account may log in) or on. Locking verifies what sshd would actually allow before it takes effect; a check failing leaves the machine unchanged.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: ptr(true)},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in lockIn) (*sdk.CallToolResult, okOut, error) {
		h, err := st.Host(ctx, in.Host)
		if err != nil {
			return nil, okOut{}, err
		}
		if err := fl.SetLock(ctx, h, in.Locked); err != nil {
			return nil, okOut{}, err
		}
		go fl.Sweep(context.Background(), h)
		return nil, okOut{OK: true}, nil
	})

	addTool(srv, &sdk.Tool{
		Name:        "rebuild_script_get",
		Description: "The host's rebuild script: the sh script kept on the hub that takes a fresh Debian to the state this host is in now, setup and data. Read it before updating it.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in hostIn) (*sdk.CallToolResult, scriptOut, error) {
		h, err := st.Host(ctx, in.Host)
		if err != nil {
			return nil, scriptOut{}, err
		}
		return nil, scriptOut{Host: h.Name, Script: h.RebuildScript}, nil
	})

	addTool(srv, &sdk.Tool{
		Name:        "rebuild_script_set",
		Description: "Replace the host's rebuild script with the whole new text. Do this after every job report: read the script, fold in each change the report lists as the commands that would make it again (idempotent, in order; small data — config files, keys, compose files, a dump the job took — inline as heredocs; larger data named at the top under what the script cannot recreate), and write it back. The hub never runs this script against an enrolled host; it runs it on a fresh machine enrolled to rebuild this one.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: ptr(false)},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in scriptIn) (*sdk.CallToolResult, okOut, error) {
		h, err := st.Host(ctx, in.Host)
		if err != nil {
			return nil, okOut{}, err
		}
		if err := st.SetRebuildScript(ctx, h.ID, in.Script); err != nil {
			return nil, okOut{}, err
		}
		fl.Notify("host.rebuild_script", h.Name, fmt.Sprintf("%s: rebuild script updated (%d bytes)", h.Name, len(in.Script)))
		return nil, okOut{OK: true}, nil
	})

	addTool(srv, &sdk.Tool{
		Name:        "model_fit",
		Description: "Ask llmfit on a host which models run well on its actual memory, CPU and GPU, ranked, with the Ollama name to pull and the speed to expect. Use it before pulling a model or pointing a host's agent at the pool.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in fitIn) (*sdk.CallToolResult, fitOut, error) {
		h, err := st.Host(ctx, in.Host)
		if err != nil {
			return nil, fitOut{}, err
		}
		fit, err := fl.Fit(ctx, h, in.Limit)
		if err != nil {
			return nil, fitOut{}, err
		}
		return nil, fitOut{Host: h.Name, Fit: fit}, nil
	})

	addTool(srv, &sdk.Tool{
		Name:        "list_secrets",
		Description: "The names of the secrets the hub holds and which hosts may read each; a job's remote reads one with `homedash-secret NAME` while the job runs. Values are never returned here or anywhere else.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, secretsOut, error) {
		secs, err := st.Secrets(ctx)
		if err != nil {
			return nil, secretsOut{}, err
		}
		return nil, secretsOut{Secrets: secs}, nil
	})
}

func ptr[T any](v T) *T { return &v }

type fitIn struct {
	Host  string `json:"host" jsonschema:"the host's name"`
	Limit int    `json:"limit,omitempty" jsonschema:"how many, default 8"`
}
type fitOut struct {
	Host string           `json:"host"`
	Fit  *fleet.FitResult `json:"fit"`
}
type secretsOut struct {
	Secrets []store.Secret `json:"secrets"`
}

type hostIn struct {
	Host string `json:"host" jsonschema:"the host's name"`
}
type hostRow struct {
	ID       int64           `json:"id"`
	Name     string          `json:"name"`
	Addr     string          `json:"addr"`
	Status   string          `json:"status"`
	Facts    json.RawMessage `json:"facts"`
	LastSeen string          `json:"lastSeen,omitempty"`
}
type hostsOut struct {
	Hosts []hostRow `json:"hosts"`
}
type lockIn struct {
	Host   string `json:"host" jsonschema:"the host's name"`
	Locked bool   `json:"locked"`
}
type okOut struct {
	OK bool `json:"ok"`
}
type scriptIn struct {
	Host   string `json:"host" jsonschema:"the host's name"`
	Script string `json:"script" jsonschema:"the whole script"`
}
type scriptOut struct {
	Host   string `json:"host"`
	Script string `json:"script"`
}
