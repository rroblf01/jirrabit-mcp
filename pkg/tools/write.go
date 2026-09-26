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

// registerIssueWriteTools wires the mutating tools that are available against
// jirrabit's current REST surface.
func registerIssueWriteTools(s *server.MCPServer, d Deps) {
	s.AddTool(mcp.NewTool("createJiraIssue",
		mcp.WithDescription("Create a new Jira work item."),
		mcp.WithTitleAnnotation("Create issue"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.CreateIssueArgs](),
	), createJiraIssue(d))

	s.AddTool(mcp.NewTool("editJiraIssue",
		mcp.WithDescription("Edit an existing work item; only fields you pass are changed. To change status, use transitionJiraIssue — jirrabit enforces its workflow there."),
		mcp.WithTitleAnnotation("Edit issue"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.EditIssueArgs](),
	), editJiraIssue(d))

	s.AddTool(mcp.NewTool("addOrEditJiraIssueComment",
		mcp.WithDescription("Add a comment, or edit an existing one. Pass commentId to replace an existing comment's body; omit it to create a new comment."),
		mcp.WithTitleAnnotation("Add or edit comment"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.AddOrEditCommentArgs](),
	), addOrEditJiraIssueComment(d))

	s.AddTool(mcp.NewTool("addOrEditJiraIssueWorklog",
		mcp.WithDescription("Log time on a work item, or edit an existing worklog. Pass worklogId to edit; omit it to log new time."),
		mcp.WithTitleAnnotation("Add or edit worklog"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.AddOrEditWorkLogArgs](),
	), addOrEditJiraIssueWorklog(d))
}

func createJiraIssue(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.CreateIssueArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Argument validation happens before the instance is resolved: a bad
		// argument is a client mistake, and reporting it as a connection
		// problem sends the caller looking in the wrong place.
		extra, err := normaliseFields(args.Fields, map[string]bool{
			"summary": true, "description": true, "story_points": true,
			"due_date": true, "issue_type_id": true, "assignee_id": true,
		})
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if args.ProjectKey == "" {
			return mcp.NewToolResultError("projectKey is required, e.g. WEB"), nil
		}
		if args.Summary == "" {
			return mcp.NewToolResultError("summary is required"), nil
		}
		client, shaper, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp createJiraIssue] %s %q", args.ProjectKey, args.Summary)

		// Built as a map rather than a struct so the free-form `fields` object
		// can be merged in, and so a key that was not passed is simply absent:
		// for a PATCH, "not sent" and "sent as empty" are different requests.
		payload := map[string]any{"summary": args.Summary}
		if args.Description != "" {
			payload["description"] = args.Description
		}
		if args.StoryPoints != nil {
			payload["story_points"] = *args.StoryPoints
		}
		if args.DueDate != "" {
			payload["due_date"] = args.DueDate
		}
		id, err := resolveIssueType(ctx, client, args.IssueTypeName, args.IssueTypeID)
		if err != nil {
			return toolError(err)
		}
		if id != nil {
			payload["issue_type_id"] = *id
		}
		if args.Assignee != "" {
			assignee, err := resolveAssignee(ctx, client, args.ProjectKey, args.Assignee)
			if err != nil {
				return toolError(err)
			}
			payload["assignee_id"] = assignee
		}
		for key, value := range extra {
			payload[key] = value
		}

		var issue jira.Issue
		path := fmt.Sprintf("projects/%s/issues/", url.PathEscape(args.ProjectKey))
		if err := client.Post(ctx, path, payload, &issue); err != nil {
			return toolError(err)
		}
		return jsonResult(shaper.Issue(issue))
	}
}

func editJiraIssue(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.EditIssueArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Validated before the instance is resolved, so an argument mistake is
		// reported as an argument mistake.
		extra, err := normaliseFields(args.Fields, map[string]bool{
			"summary": true, "description": true, "story_points": true,
			"due_date": true, "assignee_id": true,
		})
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if args.Assignee != nil && *args.Assignee == "" {
			// jirrabit has no unassign convention in the API yet, so say so
			// rather than silently leaving the assignee in place.
			return mcp.NewToolResultError(
				"Clearing an assignee is not supported by jirrabit's API yet. Assign the issue to someone else instead.",
			), nil
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		client, shaper, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp editJiraIssue] %s", args.IssueIDOrKey)

		var current jira.Issue
		if err := client.Get(ctx, fmt.Sprintf("issues/%s/", url.PathEscape(args.IssueIDOrKey)), &current); err != nil {
			return toolError(err)
		}

		// Only keys the caller actually set appear in the body. For a PATCH,
		// an absent key and an empty one mean different things, so this cannot
		// be a struct with zero values.
		payload := map[string]any{}
		if args.Summary != nil {
			payload["summary"] = *args.Summary
		}
		if args.Description != nil {
			payload["description"] = *args.Description
		}
		if args.StoryPoints != nil {
			payload["story_points"] = *args.StoryPoints
		}
		if args.DueDate != nil {
			payload["due_date"] = *args.DueDate
		}
		if args.Assignee != nil {
			id, err := resolveAssignee(ctx, client, current.Project, *args.Assignee)
			if err != nil {
				return toolError(err)
			}
			payload["assignee_id"] = id
		}
		for key, value := range extra {
			payload[key] = value
		}

		var updated jira.Issue
		path := fmt.Sprintf("issues/%s/", url.PathEscape(args.IssueIDOrKey))
		if err := client.Patch(ctx, path, payload, &updated); err != nil {
			return toolError(err)
		}
		return jsonResult(shaper.Issue(updated))
	}
}

func addOrEditJiraIssueComment(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.AddOrEditCommentArgs
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
		if args.Body == "" {
			return mcp.NewToolResultError("body is required"), nil
		}
		issuePath := fmt.Sprintf("issues/%s/", url.PathEscape(args.IssueIDOrKey))

		if args.CommentID != "" {
			// jirrabit has no PATCH endpoint for comments yet; say which call
			// is missing rather than reporting a 404 from a wrong path.
			return mcp.NewToolResultErrorf(
				"Editing an existing comment is not supported by jirrabit's API yet (it exposes no PATCH /api/v1/issues/%s/comments/{id}/). Delete-and-recreate is not offered either. To add a comment, omit commentId.",
				args.IssueIDOrKey,
			), nil
		}

		log.Printf("[jirrabit-mcp addOrEditJiraIssueComment] create on %s", args.IssueIDOrKey)
		var comment jira.Comment
		if err := client.Post(ctx, issuePath+"comments/", map[string]string{"body": args.Body}, &comment); err != nil {
			return toolError(err)
		}
		return jsonResult(shaper.Comment(comment))
	}
}

func addOrEditJiraIssueWorklog(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.AddOrEditWorkLogArgs
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

		if args.WorklogID != "" {
			return mcp.NewToolResultErrorf(
				"Editing an existing worklog is not supported by jirrabit's API yet (it exposes no PATCH /api/v1/issues/%s/worklogs/{id}/). To log time, omit worklogId.",
				args.IssueIDOrKey,
			), nil
		}

		minutes, err := worklogMinutes(args)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		log.Printf("[jirrabit-mcp addOrEditJiraIssueWorklog] %s %dm", args.IssueIDOrKey, minutes)

		var worklog jira.WorkLog
		path := fmt.Sprintf("issues/%s/worklogs/", url.PathEscape(args.IssueIDOrKey))
		payload := map[string]any{"minutes": minutes}
		if args.Comment != "" {
			payload["comment"] = args.Comment
		}
		if err := client.Post(ctx, path, payload, &worklog); err != nil {
			return toolError(err)
		}
		return jsonResult(shaper.WorkLog(worklog))
	}
}

// worklogMinutes normalises the two ways a duration can arrive. jirrabit takes
// whole minutes; Jira sends either seconds or a "2h 30m" string.
func worklogMinutes(args schema.AddOrEditWorkLogArgs) (int, error) {
	if args.TimeSpentSeconds != nil {
		if *args.TimeSpentSeconds <= 0 {
			return 0, fmt.Errorf("timeSpentSeconds must be greater than 0")
		}
		// Round up: logging 90 seconds is 2 minutes of jirrabit's time, and
		// truncating would silently lose the remainder.
		seconds := *args.TimeSpentSeconds
		return (seconds + 59) / 60, nil
	}
	if args.TimeSpent != "" {
		minutes, err := jira.ParseJiraDuration(args.TimeSpent)
		if err != nil {
			return 0, err
		}
		return minutes, nil
	}
	return 0, fmt.Errorf("provide timeSpentSeconds (or timeSpent in '2h 30m' form) to log time")
}
