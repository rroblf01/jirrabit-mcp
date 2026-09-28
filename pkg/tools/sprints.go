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

// registerSprintTools wires the always-on sprint surface: listing, reading,
// creating and updating. Deleting lives in registerSprintDeleteTools.
func registerSprintTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("listJiraSprints",
		mcp.WithDescription("List the sprints in a project, oldest first. Returns a page of results plus a nextPageToken: pass it back verbatim to get the next page, and do not parse it. A sprint holds the issues assigned to it; searchJiraIssuesUsingJql with `sprint = \"<name>\"` gets the issues."),
		mcp.WithTitleAnnotation("List sprints"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		// ListSprintsArgs, not SprintArgs: they shared one struct, so the list
		// tool also advertised a sprintId it ignores and hid the paging
		// arguments its handler actually reads. The schema is what strict.go
		// validates against, so the handler binding a struct the schema did not
		// declare meant maxResults was rejected as unknown.
		mcp.WithInputSchema[schema.ListSprintsArgs](),
	), listJiraSprints(d))

	s.AddTool(mcp.NewTool("getJiraSprint",
		mcp.WithDescription("Get one sprint by numeric id, including its goal and dates."),
		mcp.WithTitleAnnotation("Get sprint"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.SprintArgs](),
	), getJiraSprint(d))

	s.AddTool(mcp.NewTool("startJiraSprint",
		mcp.WithDescription(
			"Start a sprint, moving it from future to active. Nothing is moved or lost, so no confirmation "+
				"is needed and this is a single call."),
		mcp.WithTitleAnnotation("Start sprint"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.StartSprintArgs](),
	), startJiraSprint(d))

	s.AddTool(mcp.NewTool("closeJiraSprint",
		mcp.WithDescription(
			"Close a sprint, in two steps.\n\n"+
				"Closing is not a state flip: jirrabit moves every issue in the sprint that is not already done "+
				"to carriesTo, or back to the backlog when carriesTo is omitted. Issues already in a done status "+
				"stay on the closed sprint.\n\n"+
				"Call once to see how many issues would move and where, then get the user's agreement, then call "+
				"again with the confirmationToken. A closed sprint cannot be reopened, so check the count before "+
				"confirming."),
		mcp.WithTitleAnnotation("Close sprint"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.CloseSprintArgs](),
	), closeJiraSprint(d))

	s.AddTool(mcp.NewTool("createJiraSprint",
		mcp.WithDescription("Create a sprint in a project. It starts as `future`; moving issues into it is a separate step, by assigning them with editJiraIssue's sprintId or by JQL."),
		mcp.WithTitleAnnotation("Create sprint"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.CreateSprintArgs](),
	), createJiraSprint(d))

	s.AddTool(mcp.NewTool("updateJiraSprint",
		mcp.WithDescription("Update a sprint's name, goal or dates. Only the fields you pass are changed. Closing a sprint is not exposed here: it carries unfinished issues to another sprint or back to the backlog, and jirrabit has no endpoint that does only that."),
		mcp.WithTitleAnnotation("Update sprint"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.UpdateSprintArgs](),
	), updateJiraSprint(d))
}

// registerSprintDeleteTools wires the destructive sprint tool, gated by
// JIRRABIT_MCP_ENABLE_DELETE like deleteJiraIssue. A sprint holds the issues
// assigned to it, so removing one is not something an agent should do unprompted.
func registerSprintDeleteTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("deleteJiraSprint",
		mcp.WithDescription(
			"Delete a sprint. The issues assigned to it are not deleted; they end up with no "+
				"sprint, which puts them back in the backlog.\n\n"+
				"It cannot be undone, so the first call returns a preview of how many issues are "+
				"about to lose their sprint and a confirmation token, and changes nothing. Show "+
				"that to the user, wait for their agreement, then call again with the token.\n\n"+
				"Only available when JIRRABIT_MCP_ENABLE_DELETE is set."),
		mcp.WithTitleAnnotation("Delete sprint"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.DeleteSprintArgs](),
	), deleteJiraSprint(d))
}

// Sprint is jirrabit's sprint payload. Field names are already Jira's, so it
// needs no shaper beyond the paging envelope.
type Sprint struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Goal       string `json:"goal"`
	Status     string `json:"status"`
	StartDate  string `json:"start_date"`
	EndDate    string `json:"end_date"`
	RetroNotes string `json:"retro_notes"`
}

func listJiraSprints(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListSprintsArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if args.ProjectKeyOrID == "" {
			return mcp.NewToolResultError("projectKeyOrId is required, e.g. WEB"), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		key, err := projectKey(ctx, client, args.ProjectKeyOrID)
		if err != nil {
			return toolError(err)
		}
		page, err := resolvePage(args.StartAt, args.NextPageToken)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		size := clampSize(args.MaxResults)
		log.Printf("[jirrabit-mcp listJiraSprints] %s page=%d size=%d", key, page, size)

		// This used to send no page params and return a bare {"projectKey",
		// "values"}. jirrabit then applied its own default page size, and a
		// project with more sprints than that got a short list with no count
		// and no nextPageToken to follow — a paging loop read it as the end of
		// the data. The envelope is what makes "there are more" expressible.
		path := fmt.Sprintf("projects/%s/sprints/", url.PathEscape(key))
		items, meta, err := jira.List[Sprint](ctx, client, jira.WithPage(path, page, size))
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

func getJiraSprint(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.SprintArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if args.SprintID <= 0 {
			return mcp.NewToolResultError("sprintId is required, e.g. 2"), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp getJiraSprint] %d", args.SprintID)

		var sprint Sprint
		if err := client.Get(ctx, fmt.Sprintf("sprints/%d/", args.SprintID), &sprint); err != nil {
			return toolError(err)
		}
		return jsonResult(sprint)
	}
}

func createJiraSprint(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.CreateSprintArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if args.ProjectKeyOrID == "" {
			return mcp.NewToolResultError("projectKey is required, e.g. WEB"), nil
		}
		if args.Name == "" {
			return mcp.NewToolResultError("name is required, e.g. 'Sprint 14'"), nil
		}
		for label, value := range map[string]string{"startDate": args.StartDate, "endDate": args.EndDate} {
			if value != "" {
				if _, err := jira.ParseISODate(value); err != nil {
					return mcp.NewToolResultErrorf("%s must be YYYY-MM-DD, got %q", label, value), nil
				}
			}
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		key, err := projectKey(ctx, client, args.ProjectKeyOrID)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp createJiraSprint] %s %s", key, args.Name)

		payload := map[string]any{"name": args.Name}
		if args.Goal != "" {
			payload["goal"] = args.Goal
		}
		if args.StartDate != "" {
			payload["start_date"] = args.StartDate
		}
		if args.EndDate != "" {
			payload["end_date"] = args.EndDate
		}
		var created Sprint
		path := fmt.Sprintf("projects/%s/sprints/", url.PathEscape(key))
		if err := client.Post(ctx, path, payload, &created); err != nil {
			return toolError(err)
		}
		return jsonResult(created)
	}
}

func updateJiraSprint(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.UpdateSprintArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if args.SprintID <= 0 {
			return mcp.NewToolResultError("sprintId is required, e.g. 2"), nil
		}
		payload := map[string]any{}
		if args.Name != nil {
			if *args.Name == "" {
				return mcp.NewToolResultError("name cannot be empty"), nil
			}
			payload["name"] = *args.Name
		}
		if args.Goal != nil {
			payload["goal"] = *args.Goal
		}
		for label, value := range map[string]*string{
			"start_date": args.StartDate, "end_date": args.EndDate,
		} {
			if value == nil {
				continue
			}
			if *value == "" {
				payload[label] = nil
				continue
			}
			if _, err := jira.ParseISODate(*value); err != nil {
				return mcp.NewToolResultErrorf("%s must be YYYY-MM-DD, got %q", label, *value), nil
			}
			payload[label] = *value
		}
		if len(payload) == 0 {
			return mcp.NewToolResultError(
				"nothing to change: pass at least one of name, goal, startDate or endDate"), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp updateJiraSprint] %d", args.SprintID)

		var updated Sprint
		if err := client.Patch(ctx, fmt.Sprintf("sprints/%d/", args.SprintID), payload, &updated); err != nil {
			return toolError(err)
		}
		return jsonResult(updated)
	}
}

func deleteJiraSprint(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.DeleteSprintArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if args.SprintID <= 0 {
			return mcp.NewToolResultError("sprintId is required, e.g. 2"), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp deleteJiraSprint] %d", args.SprintID)

		c := confirmable{
			signer: newConfirmSigner(client),
			op:     "deleteJiraSprint",
			target: fmt.Sprint(args.SprintID),
			build: func() (Preview, func(*jira.Client) (any, error), error) {
				var sprint Sprint
				if err := client.Get(
					ctx, fmt.Sprintf("sprints/%d/", args.SprintID), &sprint,
				); err != nil {
					return Preview{}, nil, err
				}
				// The risk here is not the sprint row, it is the issues that
				// silently drop out of it. A preview that only counted the named
				// row would say "1" and hide the twelve that change column.
				affected, err := countJQL(ctx, client,
					fmt.Sprintf("sprint = %q AND archived = false", sprint.Name))
				if err != nil {
					return Preview{}, nil, err
				}
				preview := Preview{
					Operation: "deleteJiraSprint",
					Target:    sprint.Name,
					Summary: fmt.Sprintf(
						"delete sprint %q (id %d)", sprint.Name, args.SprintID),
					Reversible: false,
					Undo:       "",
				}
				if affected > 0 {
					preview.Cascade = []PreviewCascade{{
						Kind:  "issues returning to the backlog",
						Count: affected,
					}}
				}
				work := func(c *jira.Client) (any, error) {
					if err := c.Delete(
						ctx, fmt.Sprintf("sprints/%d/", args.SprintID), nil,
					); err != nil {
						return nil, err
					}
					return map[string]any{"deleted": true, "sprintId": args.SprintID}, nil
				}
				return preview, work, nil
			},
		}
		return c.run(args.Confirmation.Confirm, client)
	}
}

// startJiraSprint is deliberately one call. Starting a sprint flips a state and
// loses nothing, so a confirmation token would be ceremony; closing one, below,
// is not.
func startJiraSprint(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.StartSprintArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.SprintID == 0 {
			return mcp.NewToolResultError("sprintId is required, from listJiraSprints"), nil
		}
		log.Printf("[jirrabit-mcp startJiraSprint] %d on %s", args.SprintID, client.BaseURL())
		var sprint Sprint
		if err := client.Post(
			ctx, fmt.Sprintf("sprints/%d/start/", args.SprintID), nil, &sprint,
		); err != nil {
			return toolError(err)
		}
		return jsonResult(sprint)
	}
}

func closeJiraSprint(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.CloseSprintArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.SprintID == 0 {
			return mcp.NewToolResultError("sprintId is required, from listJiraSprints"), nil
		}
		target := fmt.Sprint(args.SprintID)
		log.Printf("[jirrabit-mcp closeJiraSprint] %s on %s", target, client.BaseURL())

		// The destination goes into the token's target, so a token issued for
		// "close this sprint into S2" cannot be spent closing it into S3. The
		// digest covers the counts as well, which is what catches work landing
		// on the sprint between the preview and the close.
		destination := "backlog"
		if args.CarriesTo != 0 {
			destination = fmt.Sprint(args.CarriesTo)
		}
		c := confirmable{
			signer: newConfirmSigner(client),
			op:     "closeJiraSprint",
			target: target + "->" + destination,
			build: func() (Preview, func(*jira.Client) (any, error), error) {
				var sprint Sprint
				if err := client.Get(
					ctx, fmt.Sprintf("sprints/%d/", args.SprintID), &sprint,
				); err != nil {
					return Preview{}, nil, err
				}
				unfinished, err := countUnfinished(ctx, client, sprint.Name)
				if err != nil {
					return Preview{}, nil, err
				}
				where := "back to the backlog"
				if args.CarriesTo != 0 {
					where = fmt.Sprintf("into sprint %d", args.CarriesTo)
				}
				preview := Preview{
					Operation: "closeJiraSprint",
					Target:    target,
					Summary: fmt.Sprintf("sprint %d %q, moving its %d unfinished issues %s",
						args.SprintID, sprint.Name, unfinished, where),
					Cascade: []PreviewCascade{{
						Kind:  "issues that change sprint",
						Count: unfinished,
						Detail: "only issues not already in a done status; finished ones stay on the closed " +
							"sprint, which is jirrabit's behaviour and is not a mistake",
					}},
					Reversible:  false,
					Alternative: "leave the sprint active; a closed sprint cannot be reopened",
				}
				work := func(cl *jira.Client) (any, error) {
					body := map[string]any{}
					if args.CarriesTo != 0 {
						body["carry_to"] = args.CarriesTo
					}
					var out jiraCloseSprint
					path := fmt.Sprintf("sprints/%d/close/", args.SprintID)
					// POST, not DELETE: jirrabit has no DELETE for this, and it
					// is a state change rather than a removal. PostOnce for the
					// same reason as DeleteOnce — a retried close would carry the
					// issues across twice.
					if err := cl.PostOnce(ctx, path, body, &out); err != nil {
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

// jiraCloseSprint is jirrabit's close response: the sprint plus what it moved.
type jiraCloseSprint struct {
	Sprint
	MovedCount int  `json:"moved_count"`
	MovedTo    *int `json:"moved_to"`
}

// countUnfinished counts the issues in a sprint that are not already done.
//
// By name rather than by id because the listing endpoint is the one tool an agent
// certainly has, and JQL's sprint field matches on name. A sprint whose issues
// were renamed mid-flight would report zero here; that is a wrong preview rather
// than a wrong close, and the token's digest covers the number, so a wrong
// preview cannot be confirmed into a wrong action without a fresh look.
func countUnfinished(ctx context.Context, client *jira.Client, sprintName string) (int, error) {
	if sprintName == "" {
		return 0, nil
	}
	var page struct {
		Count int `json:"count"`
	}
	jql := url.QueryEscape(fmt.Sprintf(`sprint = "%s" AND statusCategory != Done`, sprintName))
	if err := client.Get(ctx, "search?jql="+jql+"&size=1", &page); err != nil {
		return 0, err
	}
	return page.Count, nil
}
