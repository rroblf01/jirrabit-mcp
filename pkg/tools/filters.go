package tools

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/rroblf01/jirrabit-mcp/pkg/jira"
	"github.com/rroblf01/jirrabit-mcp/pkg/schema"
)

// registerSavedFilterTools wires the two small read-only lookups that have no
// Atlassian equivalent worth borrowing a name for: a user's saved searches, and
// a single user by id or username.
//
// getJiraUser is not a duplicate of getJiraCurrentUser. That one answers "who am
// I"; this one answers "who is this person", which is the question behind
// resolving an assignee id or a JQL `assignee = ...` clause.
func registerSavedFilterTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("listJiraSavedFilters",
		mcp.WithDescription("List the saved JQL searches the calling user has stored. Each carries the query, so it can be handed straight to searchJiraIssuesUsingJql."),
		mcp.WithTitleAnnotation("List saved filters"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.ListSavedFiltersArgs](),
	), listJiraSavedFilters(d))

	s.AddTool(mcp.NewTool("getJiraUser",
		mcp.WithDescription("Get one user by numeric id or username, with their display name and email. Use it to turn an assignee id into a name, or to confirm a username exists before assigning to it."),
		mcp.WithTitleAnnotation("Get user"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.GetUserArgs](),
	), getJiraUser(d))
}

// SavedFilter is jirrabit's stored-search payload. Field names already match
// Jira's, so it is passed through as-is.
type SavedFilter struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Query     string `json:"query"`
	Shared    bool   `json:"shared"`
	CreatedAt string `json:"created_at"`
}

func listJiraSavedFilters(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListSavedFiltersArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp listJiraSavedFilters]")

		var filters []SavedFilter
		if err := client.Get(ctx, "filters/", &filters); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"values": filters})
	}
}

func getJiraUser(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.GetUserArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if args.UserIDOrKey == "" {
			return mcp.NewToolResultError("userIdOrKey is required, e.g. bob_dev or 2"), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp getJiraUser] %s", args.UserIDOrKey)

		// jirrabit exposes /users/{id}/ and /users/search/?query=. A username has
		// to go through the search, which is also how an unknown name turns into
		// a clear "no such user" rather than a 404 on a numeric path.
		if id, convErr := strconv.Atoi(args.UserIDOrKey); convErr == nil {
			var user jira.User
			if err := client.Get(ctx, fmt.Sprintf("users/%d/", id), &user); err != nil {
				return toolError(err)
			}
			return jsonResult(user)
		}
		rows, _, err := jira.List[jira.User](
			ctx, client, "users/search/?size=50&query="+url.QueryEscape(args.UserIDOrKey))
		if err != nil {
			return toolError(err)
		}
		for _, row := range rows {
			if row.Username == args.UserIDOrKey {
				return jsonResult(row)
			}
		}
		return mcp.NewToolResultError(fmt.Sprintf(
			"no user named %q on this instance. listJiraProjects does not list users; use the users page, or searchJiraIssuesUsingJql with `assignee = %q` to find who has work",
			args.UserIDOrKey, args.UserIDOrKey,
		)), nil
	}
}
