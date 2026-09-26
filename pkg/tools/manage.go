package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/rroblf01/jirrabit-mcp/pkg/jira"
	"github.com/rroblf01/jirrabit-mcp/pkg/schema"
)

// registerManageTools wires project administration, gated behind
// JIRRABIT_MCP_ENABLE_MANAGE. Atlassian gates the equivalent tools the same way,
// because creating and reconfiguring spaces is an operator-level action.
func registerManageTools(s *server.MCPServer, d Deps) {
	s.AddTool(mcp.NewTool("updateJiraProject",
		mcp.WithDescription("Update settings on an existing space. Only the fields you pass are changed."),
		mcp.WithTitleAnnotation("Update project"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.UpdateProjectArgs](),
	), updateJiraProject(d))
}

func updateJiraProject(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.UpdateProjectArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		client, shaper, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.ProjectKeyOrID == "" {
			return mcp.NewToolResultError("projectKeyOrId is required, e.g. WEB"), nil
		}
		if args.Name == nil && args.Description == nil && args.Archived == nil {
			return mcp.NewToolResultError(
				"nothing to change: pass at least one of name, description or archived",
			), nil
		}

		// jirrabit keys projects by their key, so a numeric id has to be
		// translated before it can be used as a path segment.
		key := args.ProjectKeyOrID
		if !isProjectKey(args.ProjectKeyOrID) {
			resolved, err := resolveProjectKey(ctx, client, args.ProjectKeyOrID)
			if err != nil {
				return toolError(err)
			}
			key = resolved
		}

		payload := map[string]any{}
		if args.Name != nil {
			payload["name"] = *args.Name
		}
		if args.Description != nil {
			payload["description"] = *args.Description
		}
		if args.Archived != nil {
			payload["archived"] = *args.Archived
		}

		var project jira.Project
		if err := client.Patch(ctx, "projects/"+key+"/", payload, &project); err != nil {
			return toolError(err)
		}
		return jsonResult(shaper.Project(project))
	}
}
