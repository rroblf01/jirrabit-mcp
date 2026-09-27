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
		mcp.WithDescription("List the sprints in a project, newest last. A sprint holds the issues assigned to it; searchJiraIssuesUsingJql with `sprint = \"<name>\"` gets the issues."),
		mcp.WithTitleAnnotation("List sprints"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.SprintArgs](),
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
		mcp.WithDescription("Delete a sprint. The issues assigned to it are not deleted; they end up with no sprint, which puts them back in the backlog. Only available when JIRRABIT_MCP_ENABLE_DELETE is set."),
		mcp.WithTitleAnnotation("Delete sprint"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.SprintArgs](),
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
		var args schema.SprintArgs
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
		log.Printf("[jirrabit-mcp listJiraSprints] %s", key)

		var page struct {
			Items []Sprint `json:"items"`
		}
		path := fmt.Sprintf("projects/%s/sprints/", url.PathEscape(key))
		if err := client.Get(ctx, path, &page); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{
			"projectKey": key,
			"values":     page.Items,
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
		log.Printf("[jirrabit-mcp deleteJiraSprint] %d", args.SprintID)

		if err := client.Delete(ctx, fmt.Sprintf("sprints/%d/", args.SprintID), nil); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"deleted": true, "sprintId": args.SprintID})
	}
}
