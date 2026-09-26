package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/adefesioso/homedash/hub/internal/agent"
)

// proposalTools is the one way anything in the house talks to the
// project: an issue, filed by the hub, redacted and capped.
func proposalTools(srv *sdk.Server, prop *agent.Proposals) {
	addTool(srv, &sdk.Tool{
		Name:        "propose",
		Description: "At the end of a session, file one issue on the HomeDash project's repository when something about the platform itself would have made this session better: a missing tool, a job instruction that always costs a round, a catalog entry that never works. Title: the change, in a line. Body: the problem, what happened here, the change. The hub redacts host names and addresses, links an open issue with the same title instead of filing twice, and caps issues per day. Not for this house's own problems.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: ptr(false), OpenWorldHint: ptr(true)},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in proposeIn) (*sdk.CallToolResult, agent.ProposalResult, error) {
		r, err := prop.File(ctx, in.Title, in.Body)
		if err != nil {
			return nil, agent.ProposalResult{}, err
		}
		return nil, *r, nil
	})
}

type proposeIn struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}
