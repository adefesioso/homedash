// Package mcp is the hub's MCP server: the fleet as a set of typed tools.
// Two clients get them — the omp windows on the Agents tab, which have no
// other tools at all, and whatever assistant the person already runs —
// and neither is privileged: every tool resolves through the store, so
// the gate at the API applies to both alike.
//
// Tools land here as the features that own them are built; a tool that
// names a host arrives with the host table.
package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/adefesioso/homedash/hub/internal/agent"
	"github.com/adefesioso/homedash/hub/internal/apps"
	"github.com/adefesioso/homedash/hub/internal/fleet"
	"github.com/adefesioso/homedash/hub/internal/storage"
	"github.com/adefesioso/homedash/hub/internal/store"
)

// Handler serves the MCP protocol over streamable HTTP. Stateless: every
// request stands alone, which is all the tools below need.
func Handler(st *store.Store, fl *fleet.Fleet, jobs *agent.Jobs, ap *apps.Apps, sto *storage.Storage, version string) http.Handler {
	srv := sdk.NewServer(&sdk.Implementation{Name: "homedash", Version: version}, nil)
	addTool(srv, &sdk.Tool{
		Name:        "list_events",
		Description: "The hub's event log, newest first: transitions into and out of trouble across the fleet, and what the hub itself did.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in listEventsIn) (*sdk.CallToolResult, listEventsOut, error) {
		limit := in.Limit
		if limit <= 0 || limit > 500 {
			limit = 100
		}
		evs, err := st.Events(ctx, 0, limit)
		if err != nil {
			return nil, listEventsOut{}, err
		}
		return nil, listEventsOut{Events: evs}, nil
	})
	hostTools(srv, st, fl)
	deviceTools(srv, st)
	jobTools(srv, st, jobs)
	appTools(srv, st, ap, sto.Gateway)
	storageTools(srv, sto)
	return sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return srv },
		&sdk.StreamableHTTPOptions{Stateless: true})
}

// addTool registers a typed tool with an output schema that treats
// json.RawMessage as "any JSON". The SDK's own inference sees only the
// []byte underneath and writes an array-of-integers schema, so a host's
// facts, a device sighting's detail or llmfit's system block — all stored
// as raw JSON — would fail the SDK's validation of our own response.
func addTool[In, Out any](srv *sdk.Server, t *sdk.Tool, h sdk.ToolHandlerFor[In, Out]) {
	if t.OutputSchema == nil {
		rt := reflect.TypeFor[Out]()
		if rt.Kind() == reflect.Pointer {
			rt = rt.Elem()
		}
		s, err := jsonschema.ForType(rt, &jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{
			reflect.TypeFor[json.RawMessage](): {},
		}})
		if err != nil {
			panic("mcp: output schema for " + t.Name + ": " + err.Error())
		}
		t.OutputSchema = s
	}
	sdk.AddTool(srv, t, h)
}

type listEventsIn struct {
	Limit int `json:"limit,omitempty" jsonschema:"how many events to return, newest first; default 100, at most 500"`
}

type listEventsOut struct {
	Events []store.Event `json:"events"`
}
