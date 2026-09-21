package mcp

import (
	"context"
	"errors"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/adefesioso/homedash/hub/internal/apps"
	"github.com/adefesioso/homedash/hub/internal/store"
)

// appTools: stacks, the catalog, placement.
func appTools(srv *sdk.Server, st *store.Store, ap *apps.Apps, gateway apps.Gateway) {
	addTool(srv, &sdk.Tool{
		Name:        "list_apps",
		Description: "Every compose stack on every online remote and what its containers are doing, live. Nothing about an app is stored on the hub.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, appsOut, error) {
		stacks, errs, err := ap.List(ctx)
		if err != nil {
			return nil, appsOut{}, err
		}
		if stacks == nil {
			stacks = []apps.Stack{}
		}
		return nil, appsOut{Stacks: stacks, Errors: errs}, nil
	})
	addTool(srv, &sdk.Tool{
		Name:        "app_logs",
		Description: "Recent log lines from a stack's containers on a host.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in appIn) (*sdk.CallToolResult, textOut, error) {
		h, err := st.Host(ctx, in.Host)
		if err != nil {
			return nil, textOut{}, err
		}
		out, err := ap.Logs(ctx, h, in.Name, in.Lines)
		if err != nil {
			return nil, textOut{}, err
		}
		return nil, textOut{Text: out}, nil
	})
	addTool(srv, &sdk.Tool{
		Name:        "catalog",
		Description: "The app catalog: entries with a compose file, the volumes they want and a rough requirements line. Read it before writing a compose file of your own; deploying an entry is deploy_stack with its compose text.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, catalogOut, error) {
		c, err := ap.Catalog(ctx)
		if err != nil {
			return nil, catalogOut{}, err
		}
		return nil, catalogOut{Entries: c}, nil
	})
	addTool(srv, &sdk.Tool{
		Name:        "placement",
		Description: "Rank the fleet for a stack's needs: cores, memoryMB, diskGB, gpu, and optionally the storage cluster it wants. Arithmetic over what each remote reported and its disk trend; a sentence per verdict. The hub proposes; a person can overrule.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in apps.Needs) (*sdk.CallToolResult, placementOut, error) {
		vs, err := ap.Place(ctx, in, gateway)
		if err != nil {
			return nil, placementOut{}, err
		}
		return nil, placementOut{Verdicts: vs}, nil
	})
	addTool(srv, &sdk.Tool{
		Name:        "deploy_stack",
		Description: "Install or update a compose stack on a host: the compose file (and optional .env) is written under the hub account's ~/stacks/<name> and brought up. Check the catalog for an entry that fits before writing your own; run placement first unless the person named the host.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: ptr(true)},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in deployIn) (*sdk.CallToolResult, textOut, error) {
		h, err := st.Host(ctx, in.Host)
		if err != nil {
			return nil, textOut{}, err
		}
		// true: an MCP caller is always an agent — a window on the Agents
		// tab or whatever assistant the person runs — never the panel or
		// the CLI, which call apps.Deploy directly (A-1's compose gate).
		out, _, err := ap.Deploy(ctx, h, in.Name, in.Compose, in.Env, true)
		if err != nil {
			return nil, textOut{}, err
		}
		return nil, textOut{Text: out}, nil
	})
	addTool(srv, &sdk.Tool{
		Name:        "catalog_add",
		Description: "Save a catalog entry, replacing one of the same name. Give compose text, or a host: then the compose file of the stack by that name installed there is read back from the remote (the one running, not a draft) and saved; a stack's .env is never saved. Title, description, needs and volumes describe it for the next install.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: ptr(true)},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in catalogAddIn) (*sdk.CallToolResult, textOut, error) {
		compose := in.Compose
		switch {
		case in.Host != "" && compose != "":
			return nil, textOut{}, errors.New("give compose text or a host, not both")
		case in.Host != "":
			h, err := st.Host(ctx, in.Host)
			if err != nil {
				return nil, textOut{}, err
			}
			stack := in.Stack
			if stack == "" {
				stack = in.Name
			}
			compose, _, err = ap.File(ctx, h, stack)
			if err != nil {
				return nil, textOut{}, err
			}
			if strings.TrimSpace(compose) == "" {
				return nil, textOut{}, errors.New("no managed stack " + stack + " on " + in.Host + ": only a stack under ~/stacks can be saved")
			}
		}
		e := apps.Entry{Name: in.Name, Title: in.Title, Description: in.Description, Needs: in.Needs, Volumes: in.Volumes, Compose: compose}
		if e.Volumes == nil {
			e.Volumes = []string{}
		}
		if err := ap.AddCatalogEntry(ctx, e); err != nil {
			return nil, textOut{}, err
		}
		return nil, textOut{Text: "saved catalog entry " + e.Name}, nil
	})
	addTool(srv, &sdk.Tool{
		Name:        "app_action",
		Description: "start, stop, restart, pull (then up) or remove a stack on a host; remove with volumes deletes its data.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: ptr(true)},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in actionIn) (*sdk.CallToolResult, textOut, error) {
		h, err := st.Host(ctx, in.Host)
		if err != nil {
			return nil, textOut{}, err
		}
		out, err := ap.Action(ctx, h, in.Name, in.Action, in.Volumes)
		if err != nil {
			return nil, textOut{}, err
		}
		return nil, textOut{Text: out}, nil
	})
}

type appIn struct {
	Host  string `json:"host"`
	Name  string `json:"name" jsonschema:"the stack (compose project) name"`
	Lines int    `json:"lines,omitempty" jsonschema:"default 200"`
}
type deployIn struct {
	Host    string `json:"host"`
	Name    string `json:"name" jsonschema:"lowercase letters, digits, - and _"`
	Compose string `json:"compose" jsonschema:"the compose file text"`
	Env     string `json:"env,omitempty" jsonschema:"an optional .env file"`
}
type catalogAddIn struct {
	Name        string     `json:"name" jsonschema:"the entry name: lowercase letters, digits, - and _"`
	Title       string     `json:"title,omitempty"`
	Description string     `json:"description,omitempty" jsonschema:"one sentence: what it is for"`
	Needs       apps.Needs `json:"needs,omitempty" jsonschema:"a rough requirements line: cores, memoryMB, diskGB, gpu, cluster"`
	Volumes     []string   `json:"volumes,omitempty" jsonschema:"the volumes the stack wants"`
	Compose     string     `json:"compose,omitempty" jsonschema:"the compose file text; leave empty to read it from an installed stack"`
	Host        string     `json:"host,omitempty" jsonschema:"read the compose file from the stack installed on this host instead of giving it"`
	Stack       string     `json:"stack,omitempty" jsonschema:"with host: the installed stack's name, if it differs from the entry name"`
}
type actionIn struct {
	Host    string `json:"host"`
	Name    string `json:"name"`
	Action  string `json:"action" jsonschema:"start, stop, restart, pull or remove"`
	Volumes bool   `json:"volumes,omitempty" jsonschema:"with remove: delete the stack's volumes too"`
}
type appsOut struct {
	Stacks []apps.Stack `json:"stacks"`
	Errors []string     `json:"errors,omitempty"`
}
type textOut struct {
	Text string `json:"text"`
}
type catalogOut struct {
	Entries []apps.Entry `json:"entries"`
}
type placementOut struct {
	Verdicts []apps.Verdict `json:"verdicts"`
}
