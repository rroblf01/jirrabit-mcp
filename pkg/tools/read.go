package tools

import (
	"context"
	"fmt"
	"log"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/rroblf01/jirrabit-mcp/pkg/jira"
	"github.com/rroblf01/jirrabit-mcp/pkg/schema"
)

// registerReadTools wires the always-on read tools. Everything here is
// read-only, idempotent, and safe for an agent to call speculatively.
func registerReadTools(s *server.MCPServer, d Deps) {
	s.AddTool(mcp.NewTool("getJiraIssue",
		mcp.WithDescription("Get a Jira work item by ID or key."),
		mcp.WithTitleAnnotation("Get issue"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.GetIssueArgs](),
	), getJiraIssue(d))

	s.AddTool(mcp.NewTool("listJiraProjects",
		mcp.WithDescription("Get Jira spaces visible to the current user."),
		mcp.WithTitleAnnotation("List projects"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.ListProjectsArgs](),
	), listJiraProjects(d))

	s.AddTool(mcp.NewTool("getJiraCurrentUser",
		mcp.WithDescription("Details for the current Jira user, i.e. the owner of the API key this server was configured with."),
		mcp.WithTitleAnnotation("Get current user"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	), getJiraCurrentUser(d))

	s.AddTool(mcp.NewTool("listJiraIssueComments",
		mcp.WithDescription("Paginated comments for a Jira work item, oldest first."),
		mcp.WithTitleAnnotation("List issue comments"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.ListCommentsArgs](),
	), listJiraIssueComments(d))

	s.AddTool(mcp.NewTool("listJiraIssueWorklogs",
		mcp.WithDescription("Paginated worklogs (logged time) for a Jira work item, newest first."),
		mcp.WithTitleAnnotation("List worklogs"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.ListWorkLogsArgs](),
	), listJiraIssueWorklogs(d))
}

func getJiraIssue(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.GetIssueArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		log.Printf("[jirrabit-mcp getJiraIssue] %s", args.IssueIDOrKey)

		var issue jira.Issue
		if err := d.Client.Get(ctx, fmt.Sprintf("issues/%s/", url.PathEscape(args.IssueIDOrKey)), &issue); err != nil {
			return toolError(err)
		}
		return jsonResult(d.Shaper.Issue(issue))
	}
}

func listJiraProjects(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListProjectsArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		page, err := resolvePage(args.StartAt, args.NextPageToken)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		log.Printf("[jirrabit-mcp listJiraProjects] page=%d size=%d", page, clampSize(args.MaxResults))

		items, meta, err := jira.List[jira.Project](ctx, d.Client,
			jira.WithPage("projects/", page, clampSize(args.MaxResults)))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count:         meta.Count,
			StartAt:       page,
			MaxResults:    clampSize(args.MaxResults),
			NextPageToken: jira.NextToken(meta),
			Values:        d.Shaper.Projects(items),
		})
	}
}

func getJiraCurrentUser(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var user jira.User
		if err := d.Client.Get(ctx, "me/", &user); err != nil {
			return toolError(err)
		}
		return jsonResult(d.Shaper.User(user))
	}
}

func listJiraIssueComments(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListCommentsArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		page, err := resolvePage(args.StartAt, args.NextPageToken)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		log.Printf("[jirrabit-mcp listJiraIssueComments] %s page=%d", args.IssueIDOrKey, page)

		path := fmt.Sprintf("issues/%s/comments/", url.PathEscape(args.IssueIDOrKey))
		items, meta, err := jira.List[jira.Comment](ctx, d.Client, jira.WithPage(path, page, clampSize(args.MaxResults)))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count:         meta.Count,
			StartAt:       page,
			MaxResults:    clampSize(args.MaxResults),
			NextPageToken: jira.NextToken(meta),
			Values:        d.Shaper.Comments(items),
		})
	}
}

func listJiraIssueWorklogs(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListWorkLogsArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		page, err := resolvePage(args.StartAt, args.NextPageToken)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		log.Printf("[jirrabit-mcp listJiraIssueWorklogs] %s page=%d", args.IssueIDOrKey, page)

		path := fmt.Sprintf("issues/%s/worklogs/", url.PathEscape(args.IssueIDOrKey))
		items, meta, err := jira.List[jira.WorkLog](ctx, d.Client, jira.WithPage(path, page, clampSize(args.MaxResults)))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count:         meta.Count,
			StartAt:       page,
			MaxResults:    clampSize(args.MaxResults),
			NextPageToken: jira.NextToken(meta),
			Values:        d.Shaper.WorkLogs(items),
		})
	}
}
