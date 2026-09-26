package tools

import (
	"context"
	"fmt"
	"log"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/rroblf01/jirrabit-mcp/pkg/schema"
)

// registerLinkTools wires issue-link reads and writes.
func registerLinkTools(s *server.MCPServer, d Deps) {
	s.AddTool(mcp.NewTool("listJiraIssueLinkTypes",
		mcp.WithDescription("List the issue link types this instance supports. Call it before createJiraIssueLink rather than guessing a name."),
		mcp.WithTitleAnnotation("List link types"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.ListLinkTypesArgs](),
	), listJiraIssueLinkTypes(d))

	s.AddTool(mcp.NewTool("createJiraIssueLink",
		mcp.WithDescription("Create a link between two Jira work items. The link is directional: outwardIssueKey is the issue being acted on and inwardIssueKey is the other end."),
		mcp.WithTitleAnnotation("Create issue link"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.CreateLinkArgs](),
	), createJiraIssueLink(d))
}

func listJiraIssueLinkTypes(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListLinkTypesArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		var types []string
		if err := client.Get(ctx, "link-types/", &types); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"values": types})
	}
}

func createJiraIssueLink(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.CreateLinkArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.OutwardIssueKey == "" || args.InwardIssueKey == "" {
			return mcp.NewToolResultError(
				"both inwardIssueKey and outwardIssueKey are required; outwardIssueKey is the issue being linked from"), nil
		}
		log.Printf("[jirrabit-mcp createJiraIssueLink] %s %s %s",
			args.OutwardIssueKey, args.LinkTypeName, args.InwardIssueKey)

		path := fmt.Sprintf("issues/%s/links/", url.PathEscape(args.OutwardIssueKey))
		payload := map[string]any{
			"link_type":         args.LinkTypeName,
			"inward_issue_key":  args.InwardIssueKey,
			"outward_issue_key": args.OutwardIssueKey,
		}
		if args.Comment != "" {
			payload["comment"] = args.Comment
		}

		var created linkDTO
		if err := client.Post(ctx, path, payload, &created); err != nil {
			return toolError(err)
		}
		// A relative path is not a useful `self`, and the source/target come
		// back as numeric ids rather than keys. The shaper knows which instance
		// this call resolved to, so the link points at the right deployment.
		return jsonResult(map[string]any{
			"self":   fmt.Sprintf("%s/api/v1/%s", client.BaseURL(), path),
			"id":     created.ID,
			"type":   created.Type,
			"source": created.Source,
			"target": created.Target,
		})
	}
}

// linkDTO mirrors the API's link payload. jirrabit returns numeric ids here
// rather than issue keys, so an agent is told which ids it got.
type linkDTO struct {
	ID     int    `json:"id"`
	Type   string `json:"type"`
	Source int    `json:"source"`
	Target int    `json:"target"`
}
