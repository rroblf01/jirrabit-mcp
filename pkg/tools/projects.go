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

// registerProjectWriteTools wires creating a project and changing who is in it.
//
// These had endpoints and no tools, which left a client able to be handed a
// project and unable to put a second person in it — so a team could only be
// assembled by hand in the web UI.

func registerProjectWriteTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("createJiraProject",
		mcp.WithDescription(
			"Create a project.\n\n"+
				"The key is the prefix of every issue key in it, so it cannot be changed "+
				"later without renaming the project's issues. The creator is the lead unless "+
				"lead names somebody else, and the lead always resolves to project admin.\n\n"+
				"Needs the caller's API key to be superuser-adjacent enough to create projects; "+
				"if it is refused, that is the instance's policy, not a typo."),
		mcp.WithTitleAnnotation("Create project"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.CreateProjectArgs](),
	), createJiraProject(d))

	s.AddTool(mcp.NewTool("addJiraProjectMember",
		mcp.WithDescription(
			"Add a person to a project with a role. Needs admin on the project.\n\n"+
				"An existing member is updated rather than rejected, so this is safe to "+
				"call twice with the same role."),
		mcp.WithTitleAnnotation("Add project member"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.AddProjectMemberArgs](),
	), addJiraProjectMember(d))

	s.AddTool(mcp.NewTool("updateJiraProjectMember",
		mcp.WithDescription(
			"Change somebody's role in a project. Needs admin on the project.\n\n"+
				"Roles are admin, member and viewer. A viewer can read and nothing else; "+
				"demoting the last admin is refused by the instance."),
		mcp.WithTitleAnnotation("Update project member"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.UpdateProjectMemberArgs](),
	), updateJiraProjectMember(d))

	s.AddTool(mcp.NewTool("getJiraEpic",
		mcp.WithDescription(
			"Read one epic, with its summary, colour and whether it is closed.\n\n"+
				"listJiraEpics is usually enough; this is for one epic by id."),
		mcp.WithTitleAnnotation("Get epic"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.EpicArgs](),
	), getJiraEpic(d))

	s.AddTool(mcp.NewTool("updateJiraEpic",
		mcp.WithDescription(
			"Rename an epic, change its summary or colour, or close and reopen it.\n\n"+
				"Needs admin on the project. Only the fields you send are changed."),
		mcp.WithTitleAnnotation("Update epic"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.UpdateEpicArgs](),
	), updateJiraEpic(d))

}

type projectDTO struct {
	ID          int    `json:"id"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Lead        string `json:"lead"`
}

func createJiraProject(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.CreateProjectArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		key := strings.TrimSpace(strings.ToUpper(args.Key))
		if key == "" {
			return mcp.NewToolResultError("key is required, e.g. WEB"), nil
		}
		if len(key) > 10 {
			return mcp.NewToolResultError(
				fmt.Sprintf("key %q is %d characters; the limit is 10", key, len(key))), nil
		}
		if strings.TrimSpace(args.Name) == "" {
			return mcp.NewToolResultError("name is required, e.g. 'Website'"), nil
		}
		log.Printf("[jirrabit-mcp createJiraProject] %s on %s", key, client.BaseURL())
		body := map[string]any{"key": key, "name": args.Name, "description": args.Description}
		if args.Lead != "" {
			body["lead"] = args.Lead
		}
		members := make([]map[string]any, 0, len(args.Members))
		for _, member := range args.Members {
			if strings.TrimSpace(member.Username) == "" {
				return mcp.NewToolResultError("every member entry needs a username"), nil
			}
			role := member.Role
			if role == "" {
				role = "member"
			}
			members = append(members, map[string]any{"username": member.Username, "role": role})
		}
		if len(members) > 0 {
			body["members"] = members
		}
		var project projectDTO
		if err := client.Post(ctx, "projects/", body, &project); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{
			"project":        project,
			"issueKeyPrefix": project.Key + "-",
		})
	}
}

func addJiraProjectMember(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.AddProjectMemberArgs
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
		if strings.TrimSpace(args.Username) == "" {
			return mcp.NewToolResultError("username is required"), nil
		}
		role := args.Role
		if role == "" {
			role = "member"
		}
		log.Printf("[jirrabit-mcp addJiraProjectMember] %s %s as %s", key, args.Username, role)
		var member memberDTO
		path := fmt.Sprintf("projects/%s/members/", url.PathEscape(key))
		if err := client.Post(ctx, path, map[string]any{
			"username": args.Username, "role": role,
		}, &member); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"member": member})
	}
}

func updateJiraProjectMember(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.UpdateProjectMemberArgs
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
		if strings.TrimSpace(args.Username) == "" || strings.TrimSpace(args.Role) == "" {
			return mcp.NewToolResultError("username and role are both required"), nil
		}
		log.Printf("[jirrabit-mcp updateJiraProjectMember] %s %s -> %s", key, args.Username, args.Role)
		var member memberDTO
		path := fmt.Sprintf("projects/%s/members/%s/", url.PathEscape(key), url.PathEscape(args.Username))
		if err := client.Patch(ctx, path, map[string]any{"role": args.Role}, &member); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"member": member})
	}
}

func removeJiraProjectMember(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.RemoveProjectMemberArgs
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
		if strings.TrimSpace(args.Username) == "" {
			return mcp.NewToolResultError("username is required"), nil
		}
		log.Printf("[jirrabit-mcp removeJiraProjectMember] %s %s", key, args.Username)

		c := confirmable{
			signer: newConfirmSigner(client),
			op:     "removeJiraProjectMember",
			target: fmt.Sprintf("%s/%s", key, args.Username),
			build: func() (Preview, func(*jira.Client) (any, error), error) {
				// Read the member first, so the preview names a role rather than
				// saying "somebody" and leaving the caller to guess who.
				// Not jira.List: this endpoint answers with a bare JSON array of
				// members rather than the page envelope, so decoding it as a page
				// fails with a type error rather than an empty result.
				var members []memberDTO
				memberPath := fmt.Sprintf("projects/%s/members/", url.PathEscape(key))
				if err := client.Get(ctx, memberPath, &members); err != nil {
					return Preview{}, nil, err
				}
				role := ""
				for _, member := range members {
					if member.Username == args.Username {
						role = member.Role
						break
					}
				}
				if role == "" {
					return Preview{}, nil, fmt.Errorf(
						"%s no es miembro de %s, así que no hay nada que quitar", args.Username, key)
				}
				preview := Preview{
					Operation: "removeJiraProjectMember",
					Target:    args.Username,
					Summary: fmt.Sprintf("remove %s (role %s) from project %s",
						args.Username, role, key),
					Reversible: false,
					Undo: "re-add them with addJiraProjectMember, which restores the same role " +
						"but not any notification preferences they had",
					// The work stays; the access does not. Saying so is the whole
					// point of the preview.
					Cascade: []PreviewCascade{{
						Kind:  "issues and comments they wrote (kept, no longer visible to them)",
						Count: 0,
					}},
				}
				work := func(cl *jira.Client) (any, error) {
					var out map[string]any
					path := fmt.Sprintf("projects/%s/members/%s/",
						url.PathEscape(key), url.PathEscape(args.Username))
					if err := cl.Delete(ctx, path, &out); err != nil {
						return nil, err
					}
					return map[string]any{"removed": args.Username, "projectKeyOrId": key}, nil
				}
				return preview, work, nil
			},
		}
		return c.run(args.Confirmation.Confirm, client)
	}
}

func getJiraEpic(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.EpicArgs
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
		if args.EpicID <= 0 {
			return mcp.NewToolResultError("epicId is required, from listJiraEpics"), nil
		}
		var epic epicDTO
		path := fmt.Sprintf("projects/%s/epics/%d/", url.PathEscape(key), args.EpicID)
		if err := client.Get(ctx, path, &epic); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"epic": epic})
	}
}

func updateJiraEpic(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.UpdateEpicArgs
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
		// Only what was sent, so a rename does not blank the summary: the endpoint
		// is a PATCH and answering with defaults would be a PATCH that deletes.
		body := map[string]any{}
		if args.Name != "" {
			body["name"] = args.Name
		}
		if args.Summary != "" {
			body["summary"] = args.Summary
		}
		if args.Color != "" {
			body["color"] = args.Color
		}
		if args.Done != nil {
			body["done"] = *args.Done
		}
		if len(body) == 0 {
			return mcp.NewToolResultError(
				"nothing to change: give at least one of name, summary, color or done"), nil
		}
		log.Printf("[jirrabit-mcp updateJiraEpic] %s/%d %v", key, args.EpicID, body)
		var epic epicDTO
		path := fmt.Sprintf("projects/%s/epics/%d/", url.PathEscape(key), args.EpicID)
		if err := client.Patch(ctx, path, body, &epic); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"epic": epic})
	}
}

func deleteJiraSavedFilter(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.DeleteSavedFilterArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.FilterID <= 0 {
			return mcp.NewToolResultError("filterId is required, from listJiraSavedFilters"), nil
		}
		log.Printf("[jirrabit-mcp deleteJiraSavedFilter] %d", args.FilterID)
		c := confirmable{
			signer: newConfirmSigner(client),
			op:     "deleteJiraSavedFilter",
			target: fmt.Sprint(args.FilterID),
			build: func() (Preview, func(*jira.Client) (any, error), error) {
				filters, _, err := jira.List[savedFilterDTO](ctx, client,
					jira.WithPage("filters/", 1, 200))
				if err != nil {
					return Preview{}, nil, err
				}
				var found *savedFilterDTO
				for i := range filters {
					if filters[i].ID == args.FilterID {
						found = &filters[i]
						break
					}
				}
				if found == nil {
					return Preview{}, nil, fmt.Errorf(
						"no hay un filtro guardado con id %d en tus filtros", args.FilterID)
				}
				preview := Preview{
					Operation:  "deleteJiraSavedFilter",
					Target:     found.Name,
					Summary:    fmt.Sprintf("delete the saved search %q", found.Name),
					Reversible: true,
					Undo:       "recreate it with createJiraSavedFilter, using the query in this preview",
				}
				work := func(cl *jira.Client) (any, error) {
					var out map[string]any
					if err := cl.Delete(ctx, fmt.Sprintf("filters/%d/", args.FilterID), &out); err != nil {
						return nil, err
					}
					return map[string]any{"deleted": args.FilterID, "name": found.Name}, nil
				}
				return preview, work, nil
			},
		}
		return c.run(args.Confirmation.Confirm, client)
	}
}
