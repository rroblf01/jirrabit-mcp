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

// Administration: the vocabulary and the project configuration around it.
//
// Two things every tool here says, because both are ways an agent goes wrong:
//
//   - A status, priority or issue type that is in use cannot be deleted. jirrabit
//     refuses rather than detaching the value from every issue that carried it, so
//     "delete this and fix the issues" is not a plan.
//   - ``allowed_next`` being empty on a status means the workflow is OPEN, not
//     closed. Setting it to [] therefore makes a workflow *more* permissive, which
//     is the opposite of what "restrict this" sounds like.

// --- status, priority, issue type ------------------------------------------

func registerVocabularyWriteTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("createJiraStatus",
		mcp.WithDescription(
			"Create a status. Superuser only. Omit `order` to put it at the end of the board's columns; "+
				"passing 0 would put it at the front and reorder a live project.\n\n"+
				"A new status is reachable from nothing until you name its transitions, and an empty "+
				"transition list on a status means its workflow is OPEN — so a status that is not in anyone's "+
				"allowed list can still be entered, while adding one to a list restricts where it can be "+
				"reached from. Use updateJiraStatusTransitions to be explicit about which."),
		mcp.WithTitleAnnotation("Create status"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.CreateStatusArgs](),
	), createJiraStatus(d))

	s.AddTool(mcp.NewTool("updateJiraStatusTransitions",
		mcp.WithDescription(
			"Set which statuses may be reached from one. Superuser only. Send the whole list: it replaces "+
				"what was there.\n\n"+
				"READ THIS BEFORE CALLING IT. An empty list does not mean 'nowhere' — it means the workflow "+
				"is open from that status, so every status becomes reachable. That is how jirrabit models it, "+
				"and clearing the list by accident makes a strict workflow permissive. If you meant to forbid "+
				"everything, jirrabit has no such state and the tool will not invent one."),
		mcp.WithTitleAnnotation("Set status transitions"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.StatusTransitionsArgs](),
	), updateJiraStatusTransitions(d))

	s.AddTool(mcp.NewTool("deleteJiraStatus",
		mcp.WithDescription(
			"Delete a status. Superuser only, and refused with a 409 while any issue still uses it — jirrabit "+
				"will not silently strip the status from every one of them. Move the issues first."),
		mcp.WithTitleAnnotation("Delete status"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.DeleteStatusArgs](),
	), deleteJiraStatus(d))

	s.AddTool(mcp.NewTool("createJiraPriority",
		mcp.WithDescription("Create a priority. Superuser only."),
		mcp.WithTitleAnnotation("Create priority"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.CreatePriorityArgs](),
	), createJiraPriority(d))

	s.AddTool(mcp.NewTool("deleteJiraPriority",
		mcp.WithDescription(
			"Delete a priority. Superuser only, and refused with a 409 while any issue uses it."),
		mcp.WithTitleAnnotation("Delete priority"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.DeletePriorityArgs](),
	), deleteJiraPriority(d))

	s.AddTool(mcp.NewTool("createJiraIssueType",
		mcp.WithDescription("Create an issue type. Superuser only."),
		mcp.WithTitleAnnotation("Create issue type"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.CreateIssueTypeArgs](),
	), createJiraIssueType(d))

	s.AddTool(mcp.NewTool("deleteJiraIssueType",
		mcp.WithDescription(
			"Delete an issue type. Superuser only, and refused with a 409 while any issue uses it."),
		mcp.WithTitleAnnotation("Delete issue type"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.DeleteIssueTypeArgs](),
	), deleteJiraIssueType(d))
}

func createJiraStatus(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.CreateStatusArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if strings.TrimSpace(args.Name) == "" {
			return mcp.NewToolResultError("name is required, e.g. 'Blocked'"), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp createJiraStatus] %q on %s", args.Name, client.BaseURL())
		body := map[string]any{"name": args.Name, "category": args.Category}
		// Order is left out when unset, and that is the point: jirrabit then
		// appends, where sending 0 would jump the column to the front.
		if args.Order != nil {
			body["order"] = *args.Order
		}
		if args.WIPLimit != nil {
			body["wip_limit"] = *args.WIPLimit
		}
		var out statusDTO
		if err := client.Post(ctx, "statuses/", body, &out); err != nil {
			return toolError(err)
		}
		return jsonResult(out)
	}
}

func updateJiraStatusTransitions(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.StatusTransitionsArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.StatusID == 0 {
			return mcp.NewToolResultError("statusId is required, from listJiraStatuses"), nil
		}
		if args.AllowedNext == nil {
			// Distinguishing "not given" from "given as empty" is the whole point
			// of this tool, so it cannot be a plain array argument.
			return mcp.NewToolResultError(
				"allowedNext is required, and an empty list is a meaningful value: it makes the workflow " +
					"OPEN from this status, so every status becomes reachable. That is jirrabit's model of " +
					"'no restrictions', not 'nowhere'."), nil
		}
		log.Printf("[jirrabit-mcp updateJiraStatusTransitions] %d -> %v", args.StatusID, args.AllowedNext)
		var out statusDTO
		body := map[string]any{"allowed_next": args.AllowedNext}
		if err := client.Patch(ctx, fmt.Sprintf("statuses/%d/", args.StatusID), body, &out); err != nil {
			return toolError(err)
		}
		result := map[string]any{"status": out}
		if len(args.AllowedNext) == 0 {
			result["warning"] = "The transition list is now empty, which jirrabit reads as an OPEN " +
				"workflow: every status is reachable from this one. listJiraTransitions reports " +
				"open: true for it."
		}
		return jsonResult(result)
	}
}

func deleteJiraStatus(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.DeleteStatusArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		return deleteVocabulary(ctx, client, "statuses", args.StatusID, "statusId", "status")
	}
}

func deleteJiraPriority(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.DeletePriorityArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		return deleteVocabulary(ctx, client, "priorities", args.PriorityID, "priorityId", "priority")
	}
}

func deleteJiraIssueType(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.DeleteIssueTypeArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		return deleteVocabulary(ctx, client, "issue-types", args.IssueTypeID, "issueTypeId", "issue type")
	}
}

// deleteVocabulary is one delete for the three in-use-protected vocabularies. They
// behave identically — PROTECT, a 409 naming the count, superuser only — so three
// near-identical handlers would be three places for the message to drift. The id
// is passed in rather than read from a shared schema, because the three arguments
// have three different names and one schema cannot have three.
func deleteVocabulary(
	ctx context.Context, client *jira.Client, collection string, id int, argName, label string,
) (*mcp.CallToolResult, error) {
	if id == 0 {
		return mcp.NewToolResultErrorf(
			"%s is required, from the list tool for that vocabulary", argName), nil
	}
	log.Printf("[jirrabit-mcp] delete %s %d on %s", collection, id, client.BaseURL())
	var out map[string]any
	if err := client.Delete(ctx, fmt.Sprintf("%s/%d/", collection, id), &out); err != nil {
		return toolError(err)
	}
	return jsonResult(out)
}

func createJiraPriority(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.CreatePriorityArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if strings.TrimSpace(args.Name) == "" {
			return mcp.NewToolResultError("name is required, e.g. 'Urgent'"), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp createJiraPriority] %q", args.Name)
		var out priorityDTO
		if err := client.Post(ctx, "priorities/", map[string]any{
			"name": args.Name, "weight": args.Weight, "color": args.Color,
		}, &out); err != nil {
			return toolError(err)
		}
		return jsonResult(out)
	}
}

func createJiraIssueType(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.CreateIssueTypeArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if strings.TrimSpace(args.Name) == "" {
			return mcp.NewToolResultError("name is required, e.g. 'Chore'"), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp createJiraIssueType] %q", args.Name)
		var out issueTypeDTO
		if err := client.Post(ctx, "issue-types/", map[string]any{
			"name": args.Name, "category": args.Category, "icon": args.Icon, "color": args.Color,
		}, &out); err != nil {
			return toolError(err)
		}
		return jsonResult(out)
	}
}

// --- custom fields, webhooks, wiki ----------------------------------------

type customFieldDefDTO struct {
	ID       int      `json:"id"`
	Name     string   `json:"name"`
	Slug     string   `json:"slug"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Options  []string `json:"options"`
	Order    int      `json:"order"`
}

type webhookDTO struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Project     string   `json:"project"`
	Action      string   `json:"action"`
	Entity      string   `json:"entity"`
	Event       string   `json:"event"`
	StateFilter []string `json:"state_filter"`
	Active      bool     `json:"active"`
	LastStatus  *int     `json:"last_status"`
	LastError   string   `json:"last_error"`
}

func registerProjectConfigTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("listJiraCustomFields",
		mcp.WithDescription(
			"A project's custom field definitions, with their slugs. The slug is the key an issue's values are "+
				"stored under, so getJiraIssueCustomFields and setJiraIssueCustomFieldValues need it."),
		mcp.WithTitleAnnotation("List custom fields"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.GetProjectArgs](),
	), listJiraCustomFields(d))

	s.AddTool(mcp.NewTool("getJiraIssueCustomFields",
		mcp.WithDescription("An issue's custom field values, keyed by slug. Empty object when none are set."),
		mcp.WithTitleAnnotation("Get issue custom fields"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.GetIssueArgs](),
	), getJiraIssueCustomFields(d))

	s.AddTool(mcp.NewTool("setJiraIssueCustomFieldValues",
		mcp.WithDescription(
			"Set custom field values on an issue, keyed by slug from listJiraCustomFields. An unknown slug is "+
				"refused rather than stored, because a value nothing describes is one nothing will ever read. "+
				"A null removes the key."),
		mcp.WithTitleAnnotation("Set issue custom fields"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.SetCustomFieldValuesArgs](),
	), setJiraIssueCustomFieldValues(d))

	s.AddTool(mcp.NewTool("listJiraWebhooks",
		mcp.WithDescription(
			"A project's webhooks plus the instance-wide ones, with the last delivery status and error.\n\n"+
				"These do not deliver anywhere: jirrabit's webhook actions are in-process stubs that mostly log, "+
				"and there is no outbound HTTP. The configuration is real and survives, which is what this is "+
				"for — an operator can see what is defined and whether it errored."),
		mcp.WithTitleAnnotation("List webhooks"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.GetProjectArgs](),
	), listJiraWebhooks(d))

	s.AddTool(mcp.NewTool("getJiraProjectWiki",
		mcp.WithDescription(
			"A project's wiki page as Markdown. An empty body means it has never been written, which is a "+
				"normal state rather than a missing page: jirrabit holds exactly one page per project."),
		mcp.WithTitleAnnotation("Get project wiki"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.GetProjectArgs](),
	), getJiraProjectWiki(d))
}

func listJiraCustomFields(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		log.Printf("[jirrabit-mcp listJiraCustomFields] %s", key)
		items, meta, err := jira.List[customFieldDefDTO](
			ctx, client, jira.WithPage(fmt.Sprintf("projects/%s/custom-fields/", url.PathEscape(key)), 1, 200))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count: meta.Count, StartAt: 1, MaxResults: 200,
			NextPageToken: jira.NextToken(meta), Values: items,
		})
	}
}

func getJiraIssueCustomFields(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.GetIssueArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		var out struct {
			CustomFields map[string]any `json:"customFields"`
		}
		path := fmt.Sprintf("issues/%s/custom-fields/", url.PathEscape(args.IssueIDOrKey))
		if err := client.Get(ctx, path, &out); err != nil {
			return toolError(err)
		}
		if out.CustomFields == nil {
			out.CustomFields = map[string]any{}
		}
		return jsonResult(out)
	}
}

func setJiraIssueCustomFieldValues(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.SetCustomFieldValuesArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		if len(args.Values) == 0 {
			return mcp.NewToolResultError(
				"values is required, and an empty object is meaningful: it clears every custom field on the " +
					"issue. Use getJiraIssueCustomFields to see what is there first."), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp setJiraIssueCustomFieldValues] %d keys on %s",
			len(args.Values), args.IssueIDOrKey)
		var out struct {
			CustomFields map[string]any `json:"customFields"`
		}
		path := fmt.Sprintf("issues/%s/custom-fields/", url.PathEscape(args.IssueIDOrKey))
		if err := client.Patch(ctx, path, map[string]any{"customFields": args.Values}, &out); err != nil {
			return toolError(err)
		}
		return jsonResult(out)
	}
}

func listJiraWebhooks(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		log.Printf("[jirrabit-mcp listJiraWebhooks] %s", key)
		items, meta, err := jira.List[webhookDTO](
			ctx, client, jira.WithPage(fmt.Sprintf("projects/%s/webhooks/", url.PathEscape(key)), 1, 200))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count: meta.Count, StartAt: 1, MaxResults: 200,
			NextPageToken: jira.NextToken(meta), Values: items,
		})
	}
}

func getJiraProjectWiki(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		log.Printf("[jirrabit-mcp getJiraProjectWiki] %s", key)
		var out struct {
			Project   string `json:"project"`
			Body      string `json:"body"`
			UpdatedAt string `json:"updated_at"`
			UpdatedBy string `json:"updated_by"`
		}
		if err := client.Get(ctx, fmt.Sprintf("projects/%s/wiki/", url.PathEscape(key)), &out); err != nil {
			return toolError(err)
		}
		return jsonResult(out)
	}
}
