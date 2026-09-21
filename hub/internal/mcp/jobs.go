package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/adefesioso/homedash/hub/internal/agent"
	"github.com/adefesioso/homedash/hub/internal/store"
)

// jobTools start, follow and correct agent jobs on remotes.
func jobTools(srv *sdk.Server, st *store.Store, jobs *agent.Jobs) {
	addTool(srv, &sdk.Tool{
		Name:        "start_job",
		Description: "Give a remote's own agent a job: a host, a working directory on it, and instructions written as an outcome. The remote works alone on its own machine with its own tools and ends with a report. The working directory is usually the remote's own disk; a shared workspace path (see list_storage) makes the job one of several working the same files across remotes. Returns at once with the job id; follow it with job_status. Refused on a machine too small to run the agent.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: ptr(true)},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in startJobIn) (*sdk.CallToolResult, jobOut, error) {
		h, err := st.Host(ctx, in.Host)
		if err != nil {
			return nil, jobOut{}, err
		}
		j, err := jobs.Start(ctx, h, in.Cwd, in.Text, 0)
		if err != nil {
			return nil, jobOut{}, err
		}
		return nil, jobOut{Job: *j}, nil
	})

	addTool(srv, &sdk.Tool{
		Name:        "job_status",
		Description: "Where a job is: running, done, failed or needs_you, its round count, its report once there is one, and the newest events from the remote's stream. Poll this while a job runs; a job typically takes minutes.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in jobIn) (*sdk.CallToolResult, jobStatusOut, error) {
		j, err := st.Job(ctx, in.ID)
		if err != nil {
			return nil, jobStatusOut{}, err
		}
		evs, err := st.JobEvents(ctx, in.ID, in.After, 50)
		if err != nil {
			return nil, jobStatusOut{}, err
		}
		return nil, jobStatusOut{Job: *j, Events: evs}, nil
	})

	addTool(srv, &sdk.Tool{
		Name:        "job_correct",
		Description: "A follow-up turn on a finished job, on the same remote session, so the remote keeps what it learned. Rounds are capped (Settings, three by default); at the cap the job is marked needs_you instead.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: ptr(true)},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in correctIn) (*sdk.CallToolResult, jobOut, error) {
		j, err := jobs.Correct(ctx, in.ID, in.Text)
		if err != nil {
			return nil, jobOut{}, err
		}
		return nil, jobOut{Job: *j}, nil
	})

	addTool(srv, &sdk.Tool{
		Name:        "job_rollback",
		Description: "Put a host back to the snapshot the hub took before a finished job — the job's snapshot field says whether there is one: btrfs restores live, lvm merges at the host's next boot (which a person does), none means the rebuild script is the only way back.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: ptr(true)},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in jobIn) (*sdk.CallToolResult, jobOut, error) {
		j, err := jobs.Rollback(ctx, in.ID)
		if err != nil {
			return nil, jobOut{}, err
		}
		return nil, jobOut{Job: *j}, nil
	})

	addTool(srv, &sdk.Tool{
		Name:        "list_jobs",
		Description: "Recent jobs, newest first, for one host or all.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in listJobsIn) (*sdk.CallToolResult, jobsOut, error) {
		var hostID int64
		if in.Host != "" {
			h, err := st.Host(ctx, in.Host)
			if err != nil {
				return nil, jobsOut{}, err
			}
			hostID = h.ID
		}
		js, err := st.Jobs(ctx, hostID, 50)
		if err != nil {
			return nil, jobsOut{}, err
		}
		return nil, jobsOut{Jobs: js}, nil
	})
}

type startJobIn struct {
	Host string `json:"host" jsonschema:"the host's name"`
	Cwd  string `json:"cwd,omitempty" jsonschema:"working directory on the host; default the hub account's home"`
	Text string `json:"text" jsonschema:"the instructions, as an outcome"`
}
type jobIn struct {
	ID    int64 `json:"id"`
	After int64 `json:"after,omitempty" jsonschema:"return events after this event id, to follow the stream"`
}
type correctIn struct {
	ID   int64  `json:"id"`
	Text string `json:"text" jsonschema:"the correction"`
}
type listJobsIn struct {
	Host string `json:"host,omitempty"`
}
type jobOut struct {
	Job store.Job `json:"job"`
}
type jobStatusOut struct {
	Job    store.Job        `json:"job"`
	Events []store.JobEvent `json:"events"`
}
type jobsOut struct {
	Jobs []store.Job `json:"jobs"`
}
