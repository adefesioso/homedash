package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// A remote's facts as facts.sh reports them: raw JSON with nulls and
// nesting the hub never parses field by field.
const sampleFacts = `{"hostname":"remote-big","cores":4,"docker":{"version":"29.8.1"},
"disks":[{"name":"sda","fstype":null,"mountpoint":null,
"children":[{"name":"sda1","fstype":"ext4","mountpoint":"/"},{"name":"sda2","fstype":null,"mountpoint":null}]}]}`

// Every tool's output schema must accept what the tool actually returns;
// json.RawMessage fields were the trap (the SDK saw []byte and wrote
// "array of integers"), so any tool with that shape left is a bug.
func TestOutputSchemasAcceptRawJSON(t *testing.T) {
	srv := sdk.NewServer(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	hostTools(srv, nil, nil)
	deviceTools(srv, nil)
	jobTools(srv, nil, nil)
	appTools(srv, nil, nil, nil)
	storageTools(srv, nil, nil)
	proposalTools(srv, nil)

	ctx := context.Background()
	ct, stt := sdk.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, stt, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	// The hub's agent has no hands: nothing that runs a command or writes
	// a file on a machine. Work on a machine is a job.
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}
	for _, n := range []string{"run_command", "write_file"} {
		if names[n] {
			t.Errorf("%s is registered; the hub's agent dispatches jobs instead", n)
		}
	}
	for _, n := range []string{"start_job", "workspace_create", "workspace_member", "cluster_create", "cluster_add_member", "propose"} {
		if !names[n] {
			t.Errorf("%s is not registered", n)
		}
	}

	var hosts *jsonschema.Schema
	for _, tool := range tools.Tools {
		if tool.OutputSchema == nil {
			continue
		}
		raw, err := json.Marshal(tool.OutputSchema)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `"maximum":255`) {
			t.Errorf("%s: output schema still describes a json.RawMessage as a byte array", tool.Name)
		}
		if tool.Name == "list_hosts" {
			hosts = new(jsonschema.Schema)
			if err := json.Unmarshal(raw, hosts); err != nil {
				t.Fatal(err)
			}
		}
	}
	if hosts == nil {
		t.Fatal("list_hosts not registered")
	}
	res, err := hosts.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(`{"hosts":[{"id":1,"name":"remote-big","addr":"10.0.0.5","status":"online","facts":`+sampleFacts+`}]}`), &out); err != nil {
		t.Fatal(err)
	}
	if err := res.Validate(out); err != nil {
		t.Fatalf("list_hosts output rejected by its own schema: %v", err)
	}
}

// catalog_add takes compose text or a host to read it from, never both,
// and the refusal comes before any store or remote is touched.
func TestCatalogAddRefusesBothComposeAndHost(t *testing.T) {
	srv := sdk.NewServer(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	appTools(srv, nil, nil, nil)

	ctx := context.Background()
	ct, stt := sdk.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, stt, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "catalog_add", Arguments: map[string]any{
		"name": "x", "compose": "services: {}", "host": "remote-big",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("compose text and a host together were accepted")
	}
}
