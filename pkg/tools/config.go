package tools

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/rroblf01/jirrabit-mcp/pkg/schema"
)

// registerConfigTools wires the writes that change instance configuration.
//
// The creates arrived before the updates, which left a strange gap: an agent
// could add a status and then not be able to fix a typo in its name, or close
// it again, without the web UI. Each of these is the PATCH beside an existing
// POST, and they are grouped because they share a reason to be dangerous — a
// renamed status is a rename every issue's history refers to, and a reordered one
// moves a live column.

// statusDTO, priorityDTO and issueTypeDTO come from metadata.go: one declaration
// per vocabulary, shared by the read tools and these.

func registerConfigTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("updateJiraStatus",
		mcp.WithDescription(
			"Rename a status, change its category, its board position or its WIP limit.\n\n"+
				"Superuser only, like the web UI's workflow editor.\n\n"+
				"Two fields to be careful about. `category` is what decides whether an issue "+
				"in this status counts as resolved, so changing it changes what \"done\" means "+
				"across the instance. `order` is the board column position, so sending one "+
				"reorders the columns of a live project — omit it and nothing moves, which is "+
				"why renaming does not need it."),
		mcp.WithTitleAnnotation("Update status"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.UpdateStatusArgs](),
	), updateJiraStatus(d))

	s.AddTool(mcp.NewTool("updateJiraPriority",
		mcp.WithDescription(
			"Rename a priority, change its sort weight or its colour.\n\n"+
				"Superuser only. The name is limited to 20 characters, which is shorter than a "+
				"status name's and is the column's doing, not an arbitrary limit."),
		mcp.WithTitleAnnotation("Update priority"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.UpdatePriorityArgs](),
	), updateJiraPriority(d))

	s.AddTool(mcp.NewTool("updateJiraIssueType",
		mcp.WithDescription(
			"Rename an issue type, change its category, icon or colour.\n\n"+
				"Superuser only. The category groups types on the create dialog and is one of "+
				"task, bug, story or epic."),
		mcp.WithTitleAnnotation("Update issue type"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.UpdateIssueTypeArgs](),
	), updateJiraIssueType(d))

	s.AddTool(mcp.NewTool("updateJiraLabel",
		mcp.WithDescription(
			"Rename a label or change its colour.\n\n"+
				"Superuser only. Renaming changes every issue that carries it, because a "+
				"label's identity is its name — so prefer this over deleting and recreating "+
				"the same label, which would orphan every issue using it."),
		mcp.WithTitleAnnotation("Update label"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.UpdateLabelArgs](),
	), updateJiraLabel(d))

	s.AddTool(mcp.NewTool("createJiraCustomField",
		mcp.WithDescription(
			"Add a custom field to a project. Needs admin on the project.\n\n"+
				"Types are text, textarea, number, select, date, user and checkbox. The slug "+
				"is the key the value is stored under and is what setJiraIssueCustomFieldValues "+
				"uses; it is derived from the name when omitted and is unique per project."),
		mcp.WithTitleAnnotation("Create custom field"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.CreateCustomFieldArgs](),
	), createJiraCustomField(d))

	s.AddTool(mcp.NewTool("updateJiraProjectWiki",
		mcp.WithDescription(
			"Write a project's wiki page, replacing what was there. Needs admin on the project.\n\n"+
				"It is a PUT, so send the whole document. Read it with getJiraProjectWiki first: "+
				"a partial body replaces the rest rather than merging into it."),
		mcp.WithTitleAnnotation("Update project wiki"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.UpdateProjectWikiArgs](),
	), updateJiraProjectWiki(d))

	s.AddTool(mcp.NewTool("createJiraWebhook",
		mcp.WithDescription(
			"Create a webhook on a project. Needs admin on the project.\n\n"+
				"jirrabit dispatches these to in-process handlers that mostly log; there is no "+
				"outbound HTTP. Useful for wiring and for the audit trail, not for delivering "+
				"webhooks to something outside the instance."),
		mcp.WithTitleAnnotation("Create webhook"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.WebhookArgs](),
	), createJiraWebhook(d))

	s.AddTool(mcp.NewTool("updateJiraWebhook",
		mcp.WithDescription(
			"Change a webhook, or turn it off. Needs admin on the project.\n\n"+
				"Setting active to false is how you stop a hook without losing its definition — "+
				"prefer it to deleting."),
		mcp.WithTitleAnnotation("Update webhook"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.WebhookArgs](),
	), updateJiraWebhook(d))

	s.AddTool(mcp.NewTool("createJiraTeam",
		mcp.WithDescription(
			"Create a team, a broadcast list that is not a permission boundary. Any "+
				"authenticated caller may read teams; creating one needs admin.\n\n"+
				"The slug is the handle used to mention it in a comment."),
		mcp.WithTitleAnnotation("Create team"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.TeamArgs](),
	), createJiraTeam(d))

	s.AddTool(mcp.NewTool("updateJiraTeam",
		mcp.WithDescription(
			"Rename a team or replace its membership. Needs admin.\n\n"+
				"Members is the whole list, not a delta: omitting it leaves the membership "+
				"alone, and sending it replaces every member. Read the team with listJiraTeams "+
				"first, or you will drop somebody."),
		mcp.WithTitleAnnotation("Update team"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.TeamArgs](),
	), updateJiraTeam(d))
}

func updateJiraStatus(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.UpdateStatusArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.StatusID <= 0 {
			return mcp.NewToolResultError("statusId is required, from listJiraStatuses"), nil
		}
		body := map[string]any{}
		if args.Name != "" {
			body["name"] = args.Name
		}
		if args.Category != "" {
			body["category"] = args.Category
		}
		if args.Order != nil {
			body["order"] = *args.Order
		}
		if args.WipLimit != nil {
			body["wip_limit"] = *args.WipLimit
		}
		if len(body) == 0 {
			return mcp.NewToolResultError(
				"nothing to change: give at least one of name, category, order or wipLimit"), nil
		}
		log.Printf("[jirrabit-mcp updateJiraStatus] %d %v", args.StatusID, body)
		var out statusDTO
		if err := client.Patch(ctx, fmt.Sprintf("statuses/%d/", args.StatusID), body, &out); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"status": out})
	}
}

func updateJiraPriority(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.UpdatePriorityArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.PriorityID <= 0 {
			return mcp.NewToolResultError("priorityId is required, from listJiraPriorities"), nil
		}
		body := map[string]any{}
		if args.Name != "" {
			body["name"] = args.Name
		}
		if args.Weight != nil {
			body["weight"] = *args.Weight
		}
		if args.Color != "" {
			body["color"] = args.Color
		}
		if len(body) == 0 {
			return mcp.NewToolResultError(
				"nothing to change: give at least one of name, weight or color"), nil
		}
		log.Printf("[jirrabit-mcp updateJiraPriority] %d %v", args.PriorityID, body)
		var out priorityDTO
		if err := client.Patch(ctx, fmt.Sprintf("priorities/%d/", args.PriorityID), body, &out); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"priority": out})
	}
}

func updateJiraIssueType(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.UpdateIssueTypeArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.IssueTypeID <= 0 {
			return mcp.NewToolResultError(
				"issueTypeId is required, from listJiraIssueTypeMetadata"), nil
		}
		body := map[string]any{}
		if args.Name != "" {
			body["name"] = args.Name
		}
		if args.Category != "" {
			body["category"] = args.Category
		}
		if args.Icon != "" {
			body["icon"] = args.Icon
		}
		if args.Color != "" {
			body["color"] = args.Color
		}
		if len(body) == 0 {
			return mcp.NewToolResultError(
				"nothing to change: give at least one of name, category, icon or color"), nil
		}
		log.Printf("[jirrabit-mcp updateJiraIssueType] %d %v", args.IssueTypeID, body)
		var out issueTypeDTO
		if err := client.Patch(ctx, fmt.Sprintf("issue-types/%d/", args.IssueTypeID), body, &out); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"issueType": out})
	}
}

type labelPatchDTO struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

func updateJiraLabel(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.UpdateLabelArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.LabelID <= 0 {
			return mcp.NewToolResultError("labelId is required, from listJiraLabels"), nil
		}
		body := map[string]any{}
		if args.Name != "" {
			body["name"] = args.Name
		}
		if args.Color != "" {
			body["color"] = args.Color
		}
		if len(body) == 0 {
			return mcp.NewToolResultError("nothing to change: give name or color"), nil
		}
		log.Printf("[jirrabit-mcp updateJiraLabel] %d %v", args.LabelID, body)
		var out labelPatchDTO
		if err := client.Patch(ctx, fmt.Sprintf("labels/%d/", args.LabelID), body, &out); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"label": out})
	}
}

type customFieldDTO struct {
	ID       int      `json:"id"`
	Name     string   `json:"name"`
	Slug     string   `json:"slug"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Options  []string `json:"options"`
	Order    int      `json:"order"`
}

func createJiraCustomField(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.CreateCustomFieldArgs
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
		if strings.TrimSpace(args.Name) == "" {
			return mcp.NewToolResultError("name is required, e.g. 'Story points'"), nil
		}
		log.Printf("[jirrabit-mcp createJiraCustomField] %s %s (%s)",
			key, args.Name, orDefault(args.Type, "text"))
		var field customFieldDTO
		path := fmt.Sprintf("projects/%s/custom-fields/", url.PathEscape(key))
		if err := client.Post(ctx, path, map[string]any{
			"name":     args.Name,
			"slug":     args.Slug,
			"type":     orDefault(args.Type, "text"),
			"required": args.Required,
			"options":  args.Options,
			"order":    args.Order,
		}, &field); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{
			"customField": field,
			// The slug is the whole point: it is the key values are stored under,
			// so naming it in the answer saves a second call.
			"valueKey": field.Slug,
		})
	}
}

type wikiDTO struct {
	Project   string `json:"project"`
	Body      string `json:"body"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
}

func updateJiraProjectWiki(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.UpdateProjectWikiArgs
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
		log.Printf("[jirrabit-mcp updateJiraProjectWiki] %s (%d bytes)",
			key, len(args.Body))
		var out wikiDTO
		path := fmt.Sprintf("projects/%s/wiki/", url.PathEscape(key))
		if err := client.Put(ctx, path, map[string]any{"body": args.Body}, &out); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"wiki": out})
	}
}

func createJiraWebhook(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.WebhookArgs
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
		if strings.TrimSpace(args.Name) == "" {
			return mcp.NewToolResultError("name is required, e.g. 'notify on close'"), nil
		}
		log.Printf("[jirrabit-mcp createJiraWebhook] %s %s", key, args.Name)
		var hook webhookDTO
		path := fmt.Sprintf("projects/%s/webhooks/", url.PathEscape(key))
		if err := client.Post(ctx, path, webhookBody(args), &hook); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"webhook": hook})
	}
}

func updateJiraWebhook(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.WebhookArgs
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
		if args.WebhookID <= 0 {
			return mcp.NewToolResultError("webhookId is required, from listJiraWebhooks"), nil
		}
		log.Printf("[jirrabit-mcp updateJiraWebhook] %s/%d", key, args.WebhookID)
		var hook webhookDTO
		path := fmt.Sprintf("projects/%s/webhooks/%d/", url.PathEscape(key), args.WebhookID)
		if err := client.Patch(ctx, path, webhookBody(args), &hook); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"webhook": hook})
	}
}

// webhookBody is the one spelling of a hook's fields. The endpoint's PATCH takes
// the same struct as its POST, so the tools send the same body; a second mapping
// would be a second place for the two to disagree.
func webhookBody(args schema.WebhookArgs) map[string]any {
	body := map[string]any{
		"name":         args.Name,
		"action":       args.Action,
		"entity":       orDefault(args.Entity, "issue"),
		"event":        orDefault(args.Event, "issue.updated"),
		"state_filter": args.StateFilter,
	}
	if args.Active != nil {
		body["active"] = *args.Active
	} else {
		body["active"] = true
	}
	return body
}

func createJiraTeam(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.TeamArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if strings.TrimSpace(args.Slug) == "" || strings.TrimSpace(args.Name) == "" {
			return mcp.NewToolResultError("slug and name are both required"), nil
		}
		log.Printf("[jirrabit-mcp createJiraTeam] @%s", args.Slug)
		var team teamDTO
		if err := client.Post(ctx, "teams/", map[string]any{
			"slug": args.Slug, "name": args.Name,
			"description": args.Description, "members": orEmpty(args.Members),
		}, &team); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"team": team})
	}
}

func updateJiraTeam(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.TeamArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.TeamID <= 0 {
			return mcp.NewToolResultError("teamId is required, from listJiraTeams"), nil
		}
		body := map[string]any{}
		if args.Slug != "" {
			body["slug"] = args.Slug
		}
		if args.Name != "" {
			body["name"] = args.Name
		}
		if args.Description != "" {
			body["description"] = args.Description
		}
		if args.Members != nil {
			// The endpoint replaces the list. Said here so a caller that meant to
			// add one person does not silently drop the rest.
			body["members"] = args.Members
		}
		if len(body) == 0 {
			return mcp.NewToolResultError("nothing to change: give slug, name, description or members"), nil
		}
		log.Printf("[jirrabit-mcp updateJiraTeam] %d %v", args.TeamID, body)
		var team teamDTO
		if err := client.Patch(ctx, fmt.Sprintf("teams/%d/", args.TeamID), body, &team); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"team": team})
	}
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
