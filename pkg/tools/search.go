package tools

import (
	"context"
	"log"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/rroblf01/jirrabit-mcp/pkg/jira"
	"github.com/rroblf01/jirrabit-mcp/pkg/schema"
)

// registerSearchTools wires JQL search.
//
// This is the tool an agent calls first, and the one the rest of the session
// hangs off: almost every other Jira question starts with "find the issues
// where…".
func registerSearchTools(s *server.MCPServer, d Deps) {
	s.AddTool(mcp.NewTool("searchJiraIssuesUsingJql",
		mcp.WithDescription(
			"Search Jira work items using JQL. Returns a page of results plus a "+
				"nextPageToken: pass it back verbatim to get the next page, and do not "+
				"parse it."),
		mcp.WithTitleAnnotation("Search issues with JQL"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.SearchIssuesArgs](),
	), searchJiraIssuesUsingJql(d))
}

func searchJiraIssuesUsingJql(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.SearchIssuesArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, shaper, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.JQL == "" {
			return mcp.NewToolResultError("jql is required"), nil
		}
		page, err := resolvePage(0, args.NextPageToken)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		size := clampSize(args.MaxResults)

		// A JQL string is a query value, not a path segment, so it goes in the
		// query string properly encoded rather than pasted into the path.
		query := "search?jql=" + url.QueryEscape(args.JQL)
		log.Printf("[jirrabit-mcp searchJiraIssuesUsingJql] %s page=%d", args.JQL, page)

		items, meta, err := jira.List[jira.Issue](ctx, client, jira.WithPage(query, page, size))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count:         meta.Count,
			StartAt:       page,
			MaxResults:    size,
			NextPageToken: jira.NextToken(meta),
			Values:        shaper.Issues(items),
		})
	}
}
