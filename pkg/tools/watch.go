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

// registerWatchTools wires issue watching.
//
// jirrabit models watching as a set on the issue, with no notion of a
// notification preference attached, so watch and unwatch map onto add and
// remove rather than onto Jira's richer watcher configuration.
func registerWatchTools(s *server.MCPServer, d Deps) {
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
