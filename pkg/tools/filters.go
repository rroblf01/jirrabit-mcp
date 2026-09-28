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

	s.AddTool(mcp.NewTool("listJiraUsers",
		mcp.WithDescription(
			"List the users on this instance, with their usernames, display names and "+
				"emails. Call it with no arguments to see everyone, or with a query to "+
				"match on username, display name or email. This is how to find a username "+
				"to assign: getJiraUser only answers for a name you already have, and "+
				"neither listJiraProjects nor any other tool enumerates people.\n\n"+
				"The list is not filtered by project membership, so a user here may not be "+
				"assignable in a given project. jirrabit rejects an assignee who is not a "+
				"member, and the error says so."),
		mcp.WithTitleAnnotation("List users"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.ListUsersArgs](),
	), listJiraUsers(d))
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

// listJiraUsers is the directory the rest of the tool set was missing.
//
// Every other way of naming a person needed the name first: getJiraUser rejects
// an empty one, and resolveAssignee has nothing to resolve from. So an agent
// that did not already know a username — which is the normal case when the
// ticket says "assign this to Bob" and the instance calls him bob_dev — had no
// way to find one, and assigning became a guess. jirrabit's search endpoint
// lists everyone on an empty query and documents that as the picker case; this
// is the tool that makes that reachable.
func listJiraUsers(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListUsersArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		page, err := resolvePage(args.StartAt, args.NextPageToken)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		size := clampSize(args.MaxResults)
		log.Printf("[jirrabit-mcp listJiraUsers] query=%q page=%d size=%d", args.Query, page, size)

		query := "users/search/?"
		if args.Query != "" {
			query += "query=" + url.QueryEscape(args.Query) + "&"
		}
		items, meta, err := jira.List[jira.User](ctx, client, jira.WithPage(query, page, size))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count:         meta.Count,
			StartAt:       page,
			MaxResults:    size,
			NextPageToken: jira.NextToken(meta),
			Values:        items,
		})
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
			"no user named %q on this instance. Call listJiraUsers to see who is here, or "+
				"searchJiraIssuesUsingJql with `assignee = %q` to find who already has work",
			args.UserIDOrKey, args.UserIDOrKey,
		)), nil
	}
}
