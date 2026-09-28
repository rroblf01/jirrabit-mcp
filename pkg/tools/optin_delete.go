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

// registerOptInDeleteTools wires the irreversible writes that were not registered
// at all.
//
// Three of them existed as endpoints with no tool, and two of those are deletions
// of things people build up — a saved search, a team, an attachment — so an agent
// could not create or change them and had no way to tidy up either. They sit
// behind the delete flag like every other irreversible operation, and all five
// preview before acting, because the user's rule is that a delete asks twice
// whatever it is.
func registerOptInDeleteTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("deleteJiraLabel",
		mcp.WithDescription(
			"Delete a label, everywhere. Every issue carrying it loses it.\n\n"+
				"The first call returns a preview naming the issues that will lose the label, "+
				"and a confirmation token; it changes nothing. Show that to the user, wait for "+
				"their agreement, then call again with the token.\n\n"+
				"If the goal is to stop *using* a label, set active to false on its webhook "+
				"instead, or leave the label in place — deleting is only for one that is "+
				"genuinely finished with.\n\n"+
				"Superuser only, and only available with the operator's delete flag."),
		mcp.WithTitleAnnotation("Delete label"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.DeleteLabelArgs](),
	), deleteJiraLabel(d))

	s.AddTool(mcp.NewTool("deleteJiraCustomField",
		mcp.WithDescription(
			"Delete a project's custom field definition, and with it every value stored under "+
				"its slug on every issue. The values are not kept anywhere else, so this is the "+
				"one deletion here that destroys data rather than structure.\n\n"+
				"The first call returns a preview and a confirmation token and changes nothing. "+
				"Show it to the user, wait for their agreement, then call again with the token.\n\n"+
				"Needs admin on the project, and the operator's delete flag."),
		mcp.WithTitleAnnotation("Delete custom field"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.DeleteCustomFieldArgs](),
	), deleteJiraCustomField(d))

	s.AddTool(mcp.NewTool("deleteJiraWebhook",
		mcp.WithDescription(
			"Delete a project webhook. Needs admin on the project.\n\n"+
				"Prefer updateJiraWebhook with active:false, which stops the hook and keeps its "+
				"definition — there is nothing to lose by a hook firing twice that is worth "+
				"deleting the record of. Delete only when the hook should not exist at all.\n\n"+
				"The first call returns a preview and a confirmation token; show it to the "+
				"user, wait for their agreement, then call again with the token."),
		mcp.WithTitleAnnotation("Delete webhook"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.WebhookArgs](),
	), deleteJiraWebhook(d))

	s.AddTool(mcp.NewTool("deleteJiraTeam",
		mcp.WithDescription(
			"Delete a team. Superuser only.\n\n"+
				"The team's issues and comments stay exactly where they are; what stops "+
				"working is `@team:slug` mentions resolving to anybody, so anyone following "+
				"that team stops being notified by a comment that mentions it.\n\n"+
				"The first call returns a preview and a confirmation token; show it to the "+
				"user, wait for their agreement, then call again with the token."),
		mcp.WithTitleAnnotation("Delete team"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.DeleteTeamArgs](),
	), deleteJiraTeam(d))

	s.AddTool(mcp.NewTool("deleteJiraApiKey",
		mcp.WithDescription(
			"Revoke one of the caller's API keys. Superuser only.\n\n"+
				"Any outstanding confirmation tokens signed with that key stop being accepted "+
				"immediately, and every request made with it starts failing. If you are calling "+
				"this through that same key, this call is the last one it will make — including "+
				"any call of yours still in flight.\n\n"+
				"The first call returns a preview and a confirmation token; show it to the "+
				"user, wait for their agreement, then call again with the token."),
		mcp.WithTitleAnnotation("Revoke API key"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.DeleteAPIKeyArgs](),
	), deleteJiraApiKey(d))

	s.AddTool(mcp.NewTool("deleteJiraAttachment",
		mcp.WithDescription(
			"Delete one attachment from an issue, by id from listJiraIssueAttachments.\n\n"+
				"The file is gone: jirrabit stores attachments in the database rather than "+
				"object storage, so there is no copy to restore from. Nothing else on the issue "+
				"changes, and the comment it was attached to stays.\n\n"+
				"The first call returns a preview and a confirmation token; show it to the "+
				"user, wait for their agreement, then call again with the token."),
		mcp.WithTitleAnnotation("Delete attachment"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.DeleteAttachmentArgs](),
	), deleteJiraAttachment(d))
}

func deleteJiraLabel(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.DeleteLabelArgs
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
		log.Printf("[jirrabit-mcp deleteJiraLabel] %d on %s", args.LabelID, client.BaseURL())
		c := confirmable{
			signer: newConfirmSigner(client),
			op:     "deleteJiraLabel",
			target: fmt.Sprint(args.LabelID),
			build: func() (Preview, func(*jira.Client) (any, error), error) {
				labels, _, err := jira.List[labelPatchDTO](ctx, client,
					jira.WithPage("labels/", 1, 200))
				if err != nil {
					return Preview{}, nil, err
				}
				name := ""
				for _, label := range labels {
					if label.ID == args.LabelID {
						name = label.Name
						break
					}
				}
				if name == "" {
					return Preview{}, nil, fmt.Errorf("no hay una etiqueta con id %d", args.LabelID)
				}
				// Counted, because "deletes a label" and "detaches it from 340
				// issues" are very different things to be told.
				using, err := countJQL(ctx, client, fmt.Sprintf("label = %q", name))
				if err != nil {
					return Preview{}, nil, err
				}
				preview := Preview{
					Operation:  "deleteJiraLabel",
					Target:     name,
					Summary:    fmt.Sprintf("delete the label %q", name),
					Reversible: false,
					Undo: "recreate a label with the same name and re-apply it; jirrabit does not " +
						"remember which issues had it",
				}
				if using > 0 {
					preview.Cascade = []PreviewCascade{{
						Kind:  "issues that lose this label",
						Count: using,
					}}
				}
				work := func(cl *jira.Client) (any, error) {
					var out map[string]any
					if err := cl.Delete(ctx, fmt.Sprintf("labels/%d/", args.LabelID), &out); err != nil {
						return nil, err
					}
					return map[string]any{"deleted": args.LabelID, "name": name}, nil
				}
				return preview, work, nil
			},
		}
		return c.run(args.Confirmation.Confirm, client)
	}
}

func deleteJiraCustomField(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.DeleteCustomFieldArgs
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
		if args.FieldID <= 0 {
			return mcp.NewToolResultError("fieldId is required, from listJiraCustomFields"), nil
		}
		path := fmt.Sprintf("projects/%s/custom-fields/%d/", url.PathEscape(key), args.FieldID)
		log.Printf("[jirrabit-mcp deleteJiraCustomField] %s", path)
		c := confirmable{
			signer: newConfirmSigner(client),
			op:     "deleteJiraCustomField",
			target: fmt.Sprintf("%s/%d", key, args.FieldID),
			build: func() (Preview, func(*jira.Client) (any, error), error) {
				fields, _, err := jira.List[customFieldDTO](ctx, client,
					jira.WithPage(fmt.Sprintf("projects/%s/custom-fields/", url.PathEscape(key)), 1, 200))
				if err != nil {
					return Preview{}, nil, err
				}
				// By id, because that is what the caller has. The name is read off
				// the row so the confirmation says "the field 'Story points'" rather
				// than "field 14".
				var found *customFieldDTO
				for i := range fields {
					if fields[i].ID == args.FieldID {
						found = &fields[i]
						break
					}
				}
				if found == nil {
					return Preview{}, nil, fmt.Errorf(
						"el proyecto %s no tiene un campo personalizado con id %d", key, args.FieldID)
				}
				preview := Preview{
					Operation: "deleteJiraCustomField",
					Target:    found.Name,
					Summary: fmt.Sprintf("delete the custom field %q (slug %q) from project %s",
						found.Name, found.Slug, key),
					Reversible: false,
					Undo:       "",
					Cascade: []PreviewCascade{{
						Kind:  "every value stored under this slug, on every issue in the project",
						Count: 0,
						Detail: "the values are not stored anywhere else, so this destroys them " +
							"rather than hiding them",
					}},
				}
				work := func(cl *jira.Client) (any, error) {
					var out map[string]any
					if err := cl.Delete(ctx, path, &out); err != nil {
						return nil, err
					}
					return map[string]any{"deleted": args.FieldID, "slug": found.Slug}, nil
				}
				return preview, work, nil
			},
		}
		return c.run(args.Confirmation.Confirm, client)
	}
}

func deleteJiraWebhook(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		path := fmt.Sprintf("projects/%s/webhooks/%d/", url.PathEscape(key), args.WebhookID)
		log.Printf("[jirrabit-mcp deleteJiraWebhook] %s", path)
		c := confirmable{
			signer: newConfirmSigner(client),
			op:     "deleteJiraWebhook",
			target: fmt.Sprintf("%s/%d", key, args.WebhookID),
			build: func() (Preview, func(*jira.Client) (any, error), error) {
				hooks, _, err := jira.List[webhookDTO](ctx, client,
					jira.WithPage(fmt.Sprintf("projects/%s/webhooks/", url.PathEscape(key)), 1, 200))
				if err != nil {
					return Preview{}, nil, err
				}
				var found *webhookDTO
				for i := range hooks {
					if hooks[i].ID == args.WebhookID {
						found = &hooks[i]
						break
					}
				}
				if found == nil {
					return Preview{}, nil, fmt.Errorf(
						"el proyecto %s no tiene un webhook con id %d", key, args.WebhookID)
				}
				preview := Preview{
					Operation:  "deleteJiraWebhook",
					Target:     found.Name,
					Summary:    fmt.Sprintf("delete the webhook %q from project %s", found.Name, key),
					Reversible: true,
					Undo: "recreate it with createJiraWebhook using the same fields; the " +
						"firing history is not kept either way",
					// The safer operation, named because it usually is.
					Alternative: fmt.Sprintf(
						"updateJiraWebhook with webhookId %d and active:false stops it and keeps the definition",
						args.WebhookID),
				}
				work := func(cl *jira.Client) (any, error) {
					var out map[string]any
					if err := cl.Delete(ctx, path, &out); err != nil {
						return nil, err
					}
					return map[string]any{"deleted": args.WebhookID, "name": found.Name}, nil
				}
				return preview, work, nil
			},
		}
		return c.run(args.Confirmation.Confirm, client)
	}
}

func deleteJiraTeam(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.DeleteTeamArgs
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
		log.Printf("[jirrabit-mcp deleteJiraTeam] %d", args.TeamID)
		c := confirmable{
			signer: newConfirmSigner(client),
			op:     "deleteJiraTeam",
			target: fmt.Sprint(args.TeamID),
			build: func() (Preview, func(*jira.Client) (any, error), error) {
				teams, _, err := jira.List[teamDTO](ctx, client, jira.WithPage("teams/", 1, 200))
				if err != nil {
					return Preview{}, nil, err
				}
				var found *teamDTO
				for i := range teams {
					if teams[i].ID == args.TeamID {
						found = &teams[i]
						break
					}
				}
				if found == nil {
					return Preview{}, nil, fmt.Errorf("no hay un equipo con id %d", args.TeamID)
				}
				preview := Preview{
					Operation:  "deleteJiraTeam",
					Target:     found.Name,
					Summary:    fmt.Sprintf("delete the team @%s (%s)", found.Slug, found.Name),
					Reversible: true,
					Undo:       "recreate it with createJiraTeam; the membership has to be sent again",
					Cascade: []PreviewCascade{{
						Kind:   "people who stop being notified by @team mentions",
						Count:  len(found.Members),
						Detail: strings.Join(found.Members, ", "),
					}},
				}
				work := func(cl *jira.Client) (any, error) {
					var out map[string]any
					if err := cl.Delete(ctx, fmt.Sprintf("teams/%d/", args.TeamID), &out); err != nil {
						return nil, err
					}
					return map[string]any{"deleted": args.TeamID, "slug": found.Slug}, nil
				}
				return preview, work, nil
			},
		}
		return c.run(args.Confirmation.Confirm, client)
	}
}

func deleteJiraApiKey(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.DeleteAPIKeyArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.KeyID <= 0 {
			return mcp.NewToolResultError("keyId is required, from listJiraApiKeys"), nil
		}
		log.Printf("[jirrabit-mcp deleteJiraApiKey] %d on %s", args.KeyID, client.BaseURL())
		c := confirmable{
			signer: newConfirmSigner(client),
			op:     "deleteJiraApiKey",
			target: fmt.Sprint(args.KeyID),
			build: func() (Preview, func(*jira.Client) (any, error), error) {
				keys, _, err := jira.List[apiKeyDTO](ctx, client, jira.WithPage("api-keys/", 1, 200))
				if err != nil {
					return Preview{}, nil, err
				}
				var found *apiKeyDTO
				for i := range keys {
					if keys[i].ID == args.KeyID {
						found = &keys[i]
						break
					}
				}
				if found == nil {
					return Preview{}, nil, fmt.Errorf("no tienes una clave con id %d", args.KeyID)
				}
				preview := Preview{
					Operation:  "deleteJiraApiKey",
					Target:     found.Name,
					Summary:    fmt.Sprintf("revoke the API key %q (prefix %s)", found.Name, found.Prefix),
					Reversible: false,
					Undo: "mint a new key with createJiraApiKey; the old one cannot be un-revoked " +
						"and its token is not stored anywhere to be recovered",
					Cascade: []PreviewCascade{{
						Kind:  "clients using this key that start failing on their next request",
						Count: 0,
					}},
				}
				if found.LastUsedAt != "" {
					preview.Summary += fmt.Sprintf("; last used %s", found.LastUsedAt)
				}
				work := func(cl *jira.Client) (any, error) {
					var out map[string]any
					if err := cl.Delete(ctx, fmt.Sprintf("api-keys/%d/", args.KeyID), &out); err != nil {
						return nil, err
					}
					return map[string]any{"revoked": args.KeyID, "name": found.Name}, nil
				}
				return preview, work, nil
			},
		}
		return c.run(args.Confirmation.Confirm, client)
	}
}

func deleteJiraAttachment(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.DeleteAttachmentArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.AttachmentID <= 0 {
			return mcp.NewToolResultError(
				"attachmentId is required, from listJiraIssueAttachments"), nil
		}
		log.Printf("[jirrabit-mcp deleteJiraAttachment] %d on %s", args.AttachmentID, client.BaseURL())
		c := confirmable{
			signer: newConfirmSigner(client),
			op:     "deleteJiraAttachment",
			target: fmt.Sprint(args.AttachmentID),
			build: func() (Preview, func(*jira.Client) (any, error), error) {
				// The endpoint has no per-attachment impact endpoint, so the file is
				// named from the caller's own key list rather than fetched: one fewer
				// round trip for a preview, and the caller already has the list.
				name := fmt.Sprintf("attachment %d", args.AttachmentID)
				preview := Preview{
					Operation:  "deleteJiraAttachment",
					Target:     name,
					Summary:    fmt.Sprintf("delete %s", name),
					Reversible: false,
					Undo: "none: jirrabit stores the file in the database, so there is no copy " +
						"to restore from. It would have to be uploaded again with addJiraAttachment.",
				}
				work := func(cl *jira.Client) (any, error) {
					var out map[string]any
					if err := cl.Delete(ctx, fmt.Sprintf("attachments/%d/", args.AttachmentID), &out); err != nil {
						return nil, err
					}
					return map[string]any{"deleted": args.AttachmentID}, nil
				}
				return preview, work, nil
			},
		}
		return c.run(args.Confirmation.Confirm, client)
	}
}

// The two tools moved here from projects.go: both are irreversible, both already
// used the confirmation machinery, and registering them by default would have
// made two of the opt-in tools always available — which is exactly the thing the
// flag exists to prevent.
func registerProjectOptInDeleteTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("removeJiraProjectMember",
		mcp.WithDescription(
			"Remove somebody from a project, which revokes their access to everything in it.\n\n"+
				"Their issues, comments and history stay exactly where they are, so this is not "+
				"a delete of their work — but their access is gone and cannot be restored by "+
				"re-adding them. The first call returns a preview and a confirmation token; show "+
				"it to the user, wait for their agreement, then call again with the token.\n\n"+
				"Needs admin on the project."),
		mcp.WithTitleAnnotation("Remove project member"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.RemoveProjectMemberArgs](),
	), removeJiraProjectMember(d))
	s.AddTool(mcp.NewTool("deleteJiraSavedFilter",
		mcp.WithDescription(
			"Delete one of the caller's saved searches, by id from listJiraSavedFilters.\n\n"+
				"Nothing else is affected: the filter is a name and a query, and the issues it "+
				"matched are untouched. The first call returns a preview and a confirmation "+
				"token; show it to the user, wait for their agreement, then call again."),
		mcp.WithTitleAnnotation("Delete saved filter"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.DeleteSavedFilterArgs](),
	), deleteJiraSavedFilter(d))
}
