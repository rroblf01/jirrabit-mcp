package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/rroblf01/jirrabit-mcp/pkg/jira"
	"github.com/rroblf01/jirrabit-mcp/pkg/schema"
)

// issueTypeDTO, priorityDTO and statusDTO mirror the metadata endpoints. They
// are returned as-is rather than re-shaped: their whole purpose is to hand the
// caller the ids the write tools need, so every field is useful.
type issueTypeDTO struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Icon     string `json:"icon"`
	Color    string `json:"color"`
}

type priorityDTO struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Weight int    `json:"weight"`
	Color  string `json:"color"`
}

type statusDTO struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Order    int    `json:"order"`
	WIPLimit *int   `json:"wip_limit"`
}

// registerMetadataTools wires the vocabulary lookups.
//
// These exist because the write tools take numeric ids and an agent has no way
// to invent one. Without them an agent can create an issue but cannot choose its
// type, status or priority, which leaves the write tools half-usable. Call
// listJiraIssueTypeMetadata, listJiraPriorities and listJiraStatuses before
// filling in the `fields` object or an issueTypeId argument.
func registerMetadataTools(s *server.MCPServer, d Deps) {
	s.AddTool(mcp.NewTool("listJiraIssueTypeMetadata",
		mcp.WithDescription(
			"List the issue types this instance supports, with their ids. Call it before "+
				"createJiraIssue, or before setting issueTypeId in an edit."),
		mcp.WithTitleAnnotation("List issue types"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.ListLinkTypesArgs](),
	), listJiraIssueTypeMetadata(d))

	s.AddTool(mcp.NewTool("listJiraPriorities",
		mcp.WithDescription(
			"List the priorities this instance supports, with their ids. Call it before "+
				"setting priorityId in an edit."),
		mcp.WithTitleAnnotation("List priorities"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.ListLinkTypesArgs](),
	), listJiraPriorities(d))

	s.AddTool(mcp.NewTool("listJiraStatuses",
		mcp.WithDescription(
			"List the statuses a work item can be in, with their ids and their category "+
				"(todo, in_progress or done). The category is what statusCategory in JQL "+
				"refers to, and whether a status change is accepted depends on the "+
				"instance's workflow."),
		mcp.WithTitleAnnotation("List statuses"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.ListLinkTypesArgs](),
	), listJiraStatuses(d))
}

func listJiraIssueTypeMetadata(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		client, err := d.clientFor(ctx, req)
		if err != nil {
			return toolError(err)
		}
		items, _, err := jira.List[issueTypeDTO](ctx, client, "issue-types/?size=200")
		if err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"values": items})
	}
}

func listJiraPriorities(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		client, err := d.clientFor(ctx, req)
		if err != nil {
			return toolError(err)
		}
		items, _, err := jira.List[priorityDTO](ctx, client, "priorities/?size=200")
		if err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"values": items})
	}
}

func listJiraStatuses(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		client, err := d.clientFor(ctx, req)
		if err != nil {
			return toolError(err)
		}
		items, _, err := jira.List[statusDTO](ctx, client, "statuses/?size=200")
		if err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"values": items})
	}
}
