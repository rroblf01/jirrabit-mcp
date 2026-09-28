package tools

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/rroblf01/jirrabit-mcp/pkg/jira"
	"github.com/rroblf01/jirrabit-mcp/pkg/schema"
)

// Planning and membership: the tools that decide what an agent can do at all.
//
// They are grouped here rather than split across read.go and write.go because
// they share a reason to exist. jirrabit validated an assignee against project
// membership, and there was no way for a client to find out who was a member —
// so a rule the client could not satisfy, and "assign this to Bob" was a guess.
// Epics had the same shape: JQL filtered on them while nothing could read or
// write one.

// --- epics ---------------------------------------------------------------

type epicDTO struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Summary   string `json:"summary"`
	Color     string `json:"color"`
	Done      bool   `json:"done"`
	CreatedAt string `json:"created_at"`
}

func registerEpicTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("listJiraEpics",
		mcp.WithDescription(
			"List the epics of a project. Call this to get the ids to pass as epicId, and to see which "+
				"issues are grouped under which epic."),
		mcp.WithTitleAnnotation("List epics"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.ListEpicsArgs](),
	), listJiraEpics(d))

	s.AddTool(mcp.NewTool("createJiraEpic",
		mcp.WithDescription("Create an epic in a project. Needs admin on the project."),
		mcp.WithTitleAnnotation("Create epic"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.CreateEpicArgs](),
	), createJiraEpic(d))

}

// registerEpicDeleteTools is separate so the epic delete sits behind the delete
// flag like every other removal, rather than being always-on by accident.
func registerEpicDeleteTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("deleteJiraEpic",
		mcp.WithDescription(
			"Delete an epic. Its issues are kept and become unassigned rather than deleted.\n\n"+
				"The epic itself cannot be brought back and the issues lose their grouping, so the "+
				"first call returns a preview of how many issues that affects and a confirmation "+
				"token, and changes nothing. Show that to the user, wait for their agreement, then "+
				"call again with the token.\n\n"+
				"Only available with the operator's delete flag, and it needs admin on the project."),
		mcp.WithTitleAnnotation("Delete epic"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.DeleteEpicArgs](),
	), deleteJiraEpic(d))
}

func listJiraEpics(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListEpicsArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		key, err := projectKey(ctx, client, args.ProjectKeyOrID)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp listJiraEpics] %s", key)
		items, meta, err := jira.List[epicDTO](
			ctx, client, jira.WithPage(fmt.Sprintf("projects/%s/epics/", url.PathEscape(key)), 1, 200))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count: meta.Count, StartAt: 1, MaxResults: 200,
			NextPageToken: jira.NextToken(meta), Values: items,
		})
	}
}

func createJiraEpic(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.CreateEpicArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if strings.TrimSpace(args.Name) == "" {
			return mcp.NewToolResultError("name is required, e.g. 'Checkout rewrite'"), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		key, err := projectKey(ctx, client, args.ProjectKeyOrID)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp createJiraEpic] %q in %s", args.Name, key)
		var epic epicDTO
		body := map[string]any{"name": args.Name}
		if args.Summary != "" {
			body["summary"] = args.Summary
		}
		if args.Color != "" {
			body["color"] = args.Color
		}
		path := fmt.Sprintf("projects/%s/epics/", url.PathEscape(key))
		if err := client.Post(ctx, path, body, &epic); err != nil {
			return toolError(err)
		}
		return jsonResult(epic)
	}
}

func deleteJiraEpic(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.DeleteEpicArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		key, err := projectKey(ctx, client, args.ProjectKeyOrID)
		if err != nil {
			return toolError(err)
		}
		if args.EpicID == 0 {
			return mcp.NewToolResultError("epicId is required, from listJiraEpics"), nil
		}
		log.Printf("[jirrabit-mcp deleteJiraEpic] %d in %s", args.EpicID, key)
		path := fmt.Sprintf("projects/%s/epics/%d/", url.PathEscape(key), args.EpicID)

		c := confirmable{
			signer: newConfirmSigner(client),
			op:     "deleteJiraEpic",
			target: fmt.Sprintf("%s/%d", key, args.EpicID),
			build: func() (Preview, func(*jira.Client) (any, error), error) {
				var epic epicDTO
				if err := client.Get(ctx, path, &epic); err != nil {
					return Preview{}, nil, err
				}
				// The epic row is not the risk. The issues that stop being grouped
				// under it are, and a preview that counted only the named row
				// would read as "nothing much".
				affected, err := countJQL(ctx, client,
					fmt.Sprintf("project = %s AND epic = %q AND archived = false", key, epic.Name))
				if err != nil {
					return Preview{}, nil, err
				}
				preview := Preview{
					Operation: "deleteJiraEpic",
					Target:    epic.Name,
					Summary: fmt.Sprintf("delete epic %q from project %s",
						epic.Name, key),
					Reversible: false,
					Undo:       "",
				}
				if affected > 0 {
					preview.Cascade = []PreviewCascade{{
						Kind:  "issues that lose their epic",
						Count: affected,
					}}
				}
				work := func(cl *jira.Client) (any, error) {
					var out map[string]any
					if err := cl.Delete(ctx, path, &out); err != nil {
						return nil, err
					}
					return out, nil
				}
				return preview, work, nil
			},
		}
		return c.run(args.Confirmation.Confirm, client)
	}
}

// --- labels ---------------------------------------------------------------

type labelDTO struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

func registerLabelTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("listJiraLabels",
		mcp.WithDescription(
			"List every label on the instance. Labels are global, not per project. You rarely need this: "+
				"setting a label on an issue creates it if it is new, and takes a plain name."),
		mcp.WithTitleAnnotation("List labels"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.ListLinkTypesArgs](),
	), listJiraLabels(d))
}

func listJiraLabels(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		client, err := d.clientFor(ctx, req)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp listJiraLabels] %s", client.BaseURL())
		items, meta, err := jira.List[labelDTO](ctx, client, jira.WithPage("labels/", 1, 200))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count: meta.Count, StartAt: 1, MaxResults: 200,
			NextPageToken: jira.NextToken(meta), Values: items,
		})
	}
}

// --- project membership ----------------------------------------------------

type memberDTO struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	IsLead      bool   `json:"is_lead"`
}

func registerMemberTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("listJiraProjectMembers",
		mcp.WithDescription(
			"List the users of a project with their role: admin, member or viewer.\n\n"+
				"Call this before assigning work. jirrabit rejects an assignee who is not a member of the "+
				"issue's project, and this is the only way to find out who is — listJiraUsers lists everyone "+
				"on the instance without regard to any project."),
		mcp.WithTitleAnnotation("List project members"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.GetProjectArgs](),
	), listJiraProjectMembers(d))
}

func listJiraProjectMembers(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.GetProjectArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		key, err := projectKey(ctx, client, args.ProjectKeyOrID)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp listJiraProjectMembers] %s", key)
		var members []memberDTO
		path := fmt.Sprintf("projects/%s/members/", url.PathEscape(key))
		if err := client.Get(ctx, path, &members); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"projectKey": key, "values": members})
	}
}

// --- saved filters --------------------------------------------------------

type savedFilterDTO struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Query     string `json:"query"`
	Scope     string `json:"scope"`
	CreatedAt string `json:"created_at"`
}

func registerSavedFilterWriteTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("createJiraSavedFilter",
		mcp.WithDescription(
			"Save the current query under a name, so it can be recalled by listJiraSavedFilters instead of "+
				"being retyped. The query must be jirrabit's JQL subset — the same one "+
				"searchJiraIssuesUsingJql takes."),
		mcp.WithTitleAnnotation("Save a search"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.CreateSavedFilterArgs](),
	), createJiraSavedFilter(d))
}

func createJiraSavedFilter(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.CreateSavedFilterArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if strings.TrimSpace(args.Name) == "" {
			return mcp.NewToolResultError("name is required"), nil
		}
		if strings.TrimSpace(args.JQL) == "" {
			return mcp.NewToolResultError("jql is required"), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp createJiraSavedFilter] %q", args.Name)
		body := map[string]any{"name": args.Name, "query": args.JQL}
		if args.Scope != "" {
			body["scope"] = args.Scope
		}
		var saved savedFilterDTO
		if err := client.Post(ctx, "filters/", body, &saved); err != nil {
			return toolError(err)
		}
		return jsonResult(saved)
	}
}

// --- the workflow graph ----------------------------------------------------

type transitionDTO struct {
	ID       int      `json:"id"`
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Open     bool     `json:"open"`
	Next     []string `json:"next"`
}

func registerTransitionTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("listJiraTransitions",
		mcp.WithDescription(
			"List the statuses a work item in a given status can legally move to.\n\n"+
				"Call this instead of guessing and being refused. jirrabit enforces each project's workflow and "+
				"answers a forbidden transition with a 400 that names the problem but not the way out. Each row "+
				"carries `open`: when true the workflow is unrestricted and every status on the instance is "+
				"reachable, so the list is the whole set rather than a restricted one."),
		mcp.WithTitleAnnotation("List allowed transitions"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.TransitionsForStatusArgs](),
	), listJiraTransitions(d))
}

func listJiraTransitions(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.TransitionsForStatusArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.StatusID == 0 {
			return mcp.NewToolResultError(
				"statusId is required, from listJiraStatuses. Asking about the wrong status gives a list of " +
					"the wrong transitions."), nil
		}
		log.Printf("[jirrabit-mcp listJiraTransitions] %d on %s", args.StatusID, client.BaseURL())
		var rows []transitionDTO
		if err := client.Get(ctx, fmt.Sprintf("statuses/%d/transitions/", args.StatusID), &rows); err != nil {
			return toolError(err)
		}
		open := len(rows) > 0 && rows[0].Open
		return jsonResult(map[string]any{"statusId": args.StatusID, "open": open, "values": rows})
	}
}
