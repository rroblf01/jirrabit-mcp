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

// registerReadTools wires the always-on read tools. Everything here is
// read-only, idempotent, and safe for an agent to call speculatively.
func registerReadTools(s *registrar, d Deps) {
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
		mcp.WithDescription("Details for the current Jira user, i.e. the owner of the API key in use."),
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
		client, shaper, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		log.Printf("[jirrabit-mcp getJiraIssue] %s on %s", args.IssueIDOrKey, client.BaseURL())

		var issue jira.Issue
		if err := client.Get(ctx, fmt.Sprintf("issues/%s/", url.PathEscape(args.IssueIDOrKey)), &issue); err != nil {
			return toolError(err)
		}
		return jsonResult(shaper.Issue(issue))
	}
}

func listJiraProjects(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListProjectsArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, shaper, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		page, err := resolvePage(args.StartAt, args.NextPageToken)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		size := clampSize(args.MaxResults)
		log.Printf("[jirrabit-mcp listJiraProjects] %s page=%d size=%d", client.BaseURL(), page, size)

		items, meta, err := jira.List[jira.Project](ctx, client, jira.WithPage("projects/", page, size))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count:         meta.Count,
			StartAt:       page,
			MaxResults:    size,
			NextPageToken: jira.NextToken(meta),
			Values:        shaper.Projects(items),
		})
	}
}

func getJiraCurrentUser(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// This tool binds no schema, so the target is read straight off the raw
		// arguments rather than through a typed struct.
		var target schema.Target
		if err := req.BindArguments(&target); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, shaper, err := d.target(ctx, target)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp getJiraCurrentUser] %s", client.BaseURL())

		var user jira.User
		if err := client.Get(ctx, "me/", &user); err != nil {
			return toolError(err)
		}
		return jsonResult(shaper.User(user))
	}
}

func listJiraIssueComments(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListCommentsArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, shaper, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		page, err := resolvePage(args.StartAt, args.NextPageToken)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		size := clampSize(args.MaxResults)

		path := fmt.Sprintf("issues/%s/comments/", url.PathEscape(args.IssueIDOrKey))
		items, meta, err := jira.List[jira.Comment](ctx, client, jira.WithPage(path, page, size))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count:         meta.Count,
			StartAt:       page,
			MaxResults:    size,
			NextPageToken: jira.NextToken(meta),
			Values:        shaper.Comments(items),
		})
	}
}

func listJiraIssueWorklogs(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListWorkLogsArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, shaper, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		page, err := resolvePage(args.StartAt, args.NextPageToken)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		size := clampSize(args.MaxResults)

		path := fmt.Sprintf("issues/%s/worklogs/", url.PathEscape(args.IssueIDOrKey))
		items, meta, err := jira.List[jira.WorkLog](ctx, client, jira.WithPage(path, page, size))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count:         meta.Count,
			StartAt:       page,
			MaxResults:    size,
			NextPageToken: jira.NextToken(meta),
			Values:        shaper.WorkLogs(items),
		})
	}
}
