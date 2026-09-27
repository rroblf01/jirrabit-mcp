package tools

import (
	"context"
	"fmt"
	"log"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/rroblf01/jirrabit-mcp/pkg/jira"
	"github.com/rroblf01/jirrabit-mcp/pkg/schema"
)

// registerLinkTools wires issue-link reads and writes.
func registerLinkTools(s *registrar, d Deps) {
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

	s.AddTool(mcp.NewTool("getJiraIssueLinks",
		mcp.WithDescription("List the links on an issue, in both directions. Each link names the other end, so this is how you confirm a createJiraIssueLink landed or find what an issue is related to."),
		mcp.WithTitleAnnotation("Get issue links"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.GetIssueLinksArgs](),
	), getJiraIssueLinks(d))
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
		client, shaper, err := d.target(ctx, args)
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

		var created jira.Link
		if err := client.Post(ctx, path, payload, &created); err != nil {
			return toolError(err)
		}
		return jsonResult(shaper.Link(created))
	}
}

func getJiraIssueLinks(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.GetIssueLinksArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		client, shaper, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp getJiraIssueLinks] %s", args.IssueIDOrKey)

		path := fmt.Sprintf("issues/%s/links/", url.PathEscape(args.IssueIDOrKey))
		var links []jira.Link
		if err := client.Get(ctx, path, &links); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{
			"issueKey": args.IssueIDOrKey,
			"values":   shaper.Links(links),
		})
	}
}
