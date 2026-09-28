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

// registerWatchTools wires issue watching.
//
// jirrabit models watching as a set on the issue, with no notion of a
// notification preference attached, so watch and unwatch map onto add and
// remove rather than onto Jira's richer watcher configuration.
func registerWatchTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("watchJiraIssue",
		mcp.WithDescription(
			"Watch or unwatch a Jira work item. Watching means the caller is notified "+
				"of changes; it is a per-user setting, not a property of the issue."),
		mcp.WithTitleAnnotation("Watch or unwatch issue"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.WatchIssueArgs](),
	), watchJiraIssue(d))

	s.AddTool(mcp.NewTool("listJiraIssueWatchers",
		mcp.WithDescription(
			"List the usernames watching a Jira work item. Use it to see who is following "+
				"an issue before changing it, or to find out whether anyone is watching at all."),
		mcp.WithTitleAnnotation("List issue watchers"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.ListWatchersArgs](),
	), listJiraIssueWatchers(d))
}

func listJiraIssueWatchers(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListWatchersArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		log.Printf("[jirrabit-mcp listJiraIssueWatchers] %s", args.IssueIDOrKey)

		// Not paginated: jirrabit returns a bare JSON array of usernames here,
		// not its page envelope, and an issue with a thousand watchers is not
		// the case this tool exists to answer.
		var watchers []string
		if err := client.Get(
			ctx, fmt.Sprintf("issues/%s/watchers/", url.PathEscape(args.IssueIDOrKey)), &watchers,
		); err != nil {
			return toolError(err)
		}
		if watchers == nil {
			watchers = []string{}
		}
		return jsonResult(map[string]any{
			"issueIdOrKey": args.IssueIDOrKey,
			"watchers":     watchers,
		})
	}
}

func watchJiraIssue(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.WatchIssueArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		// Absent means watch, matching Jira: the tool is named watchJiraIssue,
		// so the common case should not need a flag.
		watching := true
		if args.IsWatching != nil {
			watching = *args.IsWatching
		}

		path := fmt.Sprintf("issues/%s/watchers/", url.PathEscape(args.IssueIDOrKey))
		log.Printf("[jirrabit-mcp watchJiraIssue] %s watching=%v", args.IssueIDOrKey, watching)

		var watchers []string
		if watching {
			if err := client.Post(ctx, path, nil, &watchers); err != nil {
				return toolError(err)
			}
		} else {
			if err := client.Delete(ctx, path, &watchers); err != nil {
				return toolError(err)
			}
		}
		return jsonResult(map[string]any{
			"issueIdOrKey": args.IssueIDOrKey,
			"isWatching":   watching,
			"watchers":     watchers,
		})
	}
}

// registerCommentRemovalTools wires the reversible removal of a comment.
//
// One call, no confirmation token, because jirrabit soft-deletes: `deleted_at`
// is set and the body is kept, so restoreJiraIssueComment puts it back exactly
// as it was. Requiring two steps for an undo button would be theatre.
//
// Only registered with the delete flag, though. A tool that removes something
// is one an agent should not discover unprompted, whatever it does to the row.
func registerCommentRemovalTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("deleteJiraIssueComment",
		mcp.WithDescription(
			"Delete a comment. Nothing is lost: jirrabit marks it deleted, keeps the body, and takes it out of "+
				"the issue's comments; restoreJiraIssueComment brings it back exactly as it was. "+
				"Only available because the operator enabled delete tools."),
		mcp.WithTitleAnnotation("Delete comment"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.DeleteCommentArgs](),
	), deleteJiraIssueComment(d))

	s.AddTool(mcp.NewTool("restoreJiraIssueComment",
		mcp.WithDescription("Bring back a comment deleted with deleteJiraIssueComment, with its body unchanged."),
		mcp.WithTitleAnnotation("Restore comment"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.RestoreCommentArgs](),
	), restoreJiraIssueComment(d))
}

func deleteJiraIssueComment(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.DeleteCommentArgs
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
		if args.CommentID == "" {
			return mcp.NewToolResultError("commentId is required, from listJiraIssueComments"), nil
		}
		log.Printf("[jirrabit-mcp deleteJiraIssueComment] %s on %s",
			args.CommentID, client.BaseURL())

		var comment jira.Comment
		path := fmt.Sprintf("issues/%s/comments/%s/",
			url.PathEscape(args.IssueIDOrKey), url.PathEscape(args.CommentID))
		// Not DeleteOnce: repeating it is harmless, since deleting an already
		// deleted comment returns it unchanged rather than an error.
		if err := client.Delete(ctx, path, &comment); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{
			"issueIdOrKey": args.IssueIDOrKey,
			"commentId":    args.CommentID,
			"deleted":      true,
			"reversible":   true,
			"undo":         "restoreJiraIssueComment with the same commentId",
			"comment":      shaper.Comment(comment),
		})
	}
}

func restoreJiraIssueComment(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.RestoreCommentArgs
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
		if args.CommentID == "" {
			return mcp.NewToolResultError("commentId is required, from listJiraIssueComments"), nil
		}
		log.Printf("[jirrabit-mcp restoreJiraIssueComment] %s on %s",
			args.CommentID, client.BaseURL())

		var comment jira.Comment
		path := fmt.Sprintf("issues/%s/comments/%s/restore/",
			url.PathEscape(args.IssueIDOrKey), url.PathEscape(args.CommentID))
		if err := client.Post(ctx, path, nil, &comment); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{
			"issueIdOrKey": args.IssueIDOrKey,
			"commentId":    args.CommentID,
			"restored":     true,
			"comment":      shaper.Comment(comment),
		})
	}
}
