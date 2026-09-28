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

// registerAccountTools wires the instance-wide writes: API keys, users and
// registration invites.
//
// All three are superuser-only, which jirrabit enforces. The tools say so in
// their descriptions rather than letting an agent discover it from a 403, because
// "you need admin for that" is a plan change and not an error to retry.

func registerAccountTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("listJiraApiKeys",
		mcp.WithDescription(
			"List the caller's own API keys: their names, prefixes, and when each was last "+
				"used or revoked. The secret is never returned — only a prefix — so a key that "+
				"has been lost can only be replaced, not read back."),
		mcp.WithTitleAnnotation("List API keys"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.ListAPIKeysArgs](),
	), listJiraApiKeys(d))

	s.AddTool(mcp.NewTool("createJiraApiKey",
		mcp.WithDescription(
			"Mint an API key for the caller. Superuser only.\n\n"+
				"This is the only time the token exists as text: jirrabit stores a hash. If the "+
				"answer is lost the key has to be revoked and another minted — it cannot be "+
				"retrieved. Store it before doing anything else with it."),
		mcp.WithTitleAnnotation("Create API key"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.CreateAPIKeyArgs](),
	), createJiraApiKey(d))

	s.AddTool(mcp.NewTool("listJiraAdminUsers",
		mcp.WithDescription(
			"List every user on the instance, with privilege flags. Superuser only.\n\n"+
				"This is not the same as listJiraUsers, which any caller may use and which "+
				"reports only what an assignee lookup needs. This one shows who is a "+
				"superuser and who is deactivated, so it is restricted."),
		mcp.WithTitleAnnotation("List users (admin)"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.ListAdminUsersArgs](),
	), listJiraAdminUsers(d))

	s.AddTool(mcp.NewTool("createJiraAdminUser",
		mcp.WithDescription(
			"Create a user account. Superuser only.\n\n"+
				"Omit the password and jirrabit creates one that cannot be logged into, so the "+
				"person has to reset it — which is the right default for a user created by a "+
				"script. Set one explicitly only when the caller is handing it over directly.\n\n"+
				"isSuperuser grants instance-wide admin, which bypasses every project permission "+
				"check. Read it before setting it."),
		mcp.WithTitleAnnotation("Create user (admin)"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.CreateAdminUserArgs](),
	), createJiraAdminUser(d))

	s.AddTool(mcp.NewTool("updateJiraAdminUser",
		mcp.WithDescription(
			"Change a user: email, display name, superuser flag, or whether the account is "+
				"active. Superuser only.\n\n"+
				"Setting isActive to false deactivates rather than deletes, so the person's "+
				"issues, comments and history stay exactly where they are and can be restored "+
				"by setting it back to true. Prefer it to a delete, which is not offered here "+
				"because it would take that history with it."),
		mcp.WithTitleAnnotation("Update user (admin)"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.UpdateAdminUserArgs](),
	), updateJiraAdminUser(d))

	s.AddTool(mcp.NewTool("updateJiraCurrentUser",
		mcp.WithDescription(
			"Update the caller's own jirrabit profile: display name, email, job title, "+
				"timezone, language, colour palette, whether notification emails are sent, "+
				"which notification kinds are muted, or the avatar.\n\n"+
				"This is the user behind the apiKey, not anybody else: there is no user id "+
				"argument because the caller can only ever be themselves. Changing someone "+
				"else's account is updateJiraAdminUser and needs a superuser.\n\n"+
				"Only what you send changes, and an empty call is refused with the list of "+
				"what it accepts. mutedKinds takes kind names and replaces the whole mute "+
				"list, so read getJiraCurrentUser first or you will unmute what was muted.\n\n"+
				"What it cannot do is deliberate rather than missing: username is the login "+
				"and never changes here, there is no password field because rotation stays "+
				"in the web flow, and privilege flags belong to updateJiraAdminUser."),
		mcp.WithTitleAnnotation("Update own profile"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.UpdateCurrentUserArgs](),
	), updateJiraCurrentUser(d))

	s.AddTool(mcp.NewTool("listJiraInvites",
		mcp.WithDescription(
			"List the instance's registration invites, with who made them, when they expire "+
				"and whether they were used. Superuser only.\n\n"+
				"The tokens are not included. A list that handed them out would be a page of "+
				"bearer credentials for anyone who could read it, and the token is only needed "+
				"in the moment it is minted."),
		mcp.WithTitleAnnotation("List invites"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.ListInvitesArgs](),
	), listJiraInvites(d))

	s.AddTool(mcp.NewTool("createJiraInvite",
		mcp.WithDescription(
			"Mint a registration invite. Superuser only.\n\n"+
				"This is the only response that carries the token and the registration URL, and "+
				"the only way to obtain either. It is a one-time credential that lets one "+
				"anonymous person register, so pass it on immediately and do not log it.\n\n"+
				"Only useful on an instance with invite-only registration on; with registration "+
				"open, the invite is redundant."),
		mcp.WithTitleAnnotation("Create invite"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.CreateInviteArgs](),
	), createJiraInvite(d))

	s.AddTool(mcp.NewTool("revokeJiraInvite",
		mcp.WithDescription(
			"Revoke an unused registration invite, by id from listJiraInvites. Superuser only.\n\n"+
				"The invite is expired rather than deleted, so a registration already in flight "+
				"fails as \"expired\" instead of \"no such invite\", and the row stays as a "+
				"record. Revoking is not rotation: to issue a replacement, mint a new invite."),
		mcp.WithTitleAnnotation("Revoke invite"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.RevokeInviteArgs](),
	), revokeJiraInvite(d))

	s.AddTool(mcp.NewTool("getJiraCommentHistory",
		mcp.WithDescription(
			"The previous bodies of one comment, newest first.\n\n"+
				"Incomplete by construction, and the tool says so: only two code paths in "+
				"jirrabit write a previous version, so an edit made anywhere else leaves no "+
				"trace here. Do not present this as a full edit history.\n\n"+
				"Distinct from getJiraIssueChangelog, which is the issue's field history."),
		mcp.WithTitleAnnotation("Get comment history"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.GetCommentHistoryArgs](),
	), getJiraCommentHistory(d))
}

type apiKeyDTO struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at"`
	RevokedAt  string `json:"revoked_at"`
	Active     bool   `json:"active"`
}

func listJiraApiKeys(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListAPIKeysArgs
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
		items, meta, err := jira.List[apiKeyDTO](ctx, client, jira.WithPage("api-keys/", page, size))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count: meta.Count, StartAt: page, MaxResults: size,
			NextPageToken: jira.NextToken(meta), Values: items,
		})
	}
}

func createJiraApiKey(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.CreateAPIKeyArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if strings.TrimSpace(args.Name) == "" {
			return mcp.NewToolResultError("name is required, e.g. 'ci'"), nil
		}
		log.Printf("[jirrabit-mcp createJiraApiKey] %s on %s", args.Name, client.BaseURL())
		var out map[string]any
		if err := client.Post(ctx, "api-keys/", map[string]any{"name": args.Name}, &out); err != nil {
			return toolError(err)
		}
		return jsonResult(out)
	}
}

type adminUserDTO struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	IsSuperuser bool   `json:"is_superuser"`
	IsActive    bool   `json:"is_active"`
	DateJoined  string `json:"date_joined"`
	LastLogin   string `json:"last_login"`
}

func listJiraAdminUsers(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListAdminUsersArgs
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
		path := "admin/users/"
		if args.Query != "" {
			path += "?q=" + url.QueryEscape(args.Query)
		}
		items, meta, err := jira.List[adminUserDTO](ctx, client, jira.WithPage(path, page, size))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count: meta.Count, StartAt: page, MaxResults: size,
			NextPageToken: jira.NextToken(meta), Values: items,
		})
	}
}

func createJiraAdminUser(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.CreateAdminUserArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if strings.TrimSpace(args.Username) == "" {
			return mcp.NewToolResultError("username is required"), nil
		}
		log.Printf("[jirrabit-mcp createJiraAdminUser] %s on %s", args.Username, client.BaseURL())
		body := map[string]any{
			"username":     args.Username,
			"email":        args.Email,
			"display_name": args.DisplayName,
			"is_superuser": args.IsSuperuser,
			"is_active":    true,
		}
		if args.Password != "" {
			body["password"] = args.Password
		}
		var user adminUserDTO
		if err := client.Post(ctx, "admin/users/", body, &user); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{
			"user": user,
			// Worth saying out loud, because the alternative is an account nobody
			// can log into and a support question about it.
			"note": "with no password given, jirrabit created one that cannot be logged into; " +
				"the person must reset it",
		})
	}
}

func updateJiraAdminUser(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.UpdateAdminUserArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.UserID <= 0 {
			return mcp.NewToolResultError("userId is required, from listJiraAdminUsers"), nil
		}
		// Only what was sent. Sending is_active:false alongside a display name
		// would deactivate somebody who asked to be renamed.
		body := map[string]any{}
		if args.Email != nil {
			body["email"] = *args.Email
		}
		if args.DisplayName != nil {
			body["display_name"] = *args.DisplayName
		}
		if args.IsSuperuser != nil {
			body["is_superuser"] = *args.IsSuperuser
		}
		if args.IsActive != nil {
			body["is_active"] = *args.IsActive
		}
		if len(body) == 0 {
			return mcp.NewToolResultError(
				"nothing to change: give at least one of email, displayName, isSuperuser or isActive"), nil
		}
		log.Printf("[jirrabit-mcp updateJiraAdminUser] %d %v", args.UserID, body)
		var user adminUserDTO
		if err := client.Patch(ctx, fmt.Sprintf("admin/users/%d/", args.UserID), body, &user); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"user": user})
	}
}

type meDTO struct {
	ID          int      `json:"id"`
	Username    string   `json:"username"`
	DisplayName string   `json:"display_name"`
	FirstName   string   `json:"first_name"`
	LastName    string   `json:"last_name"`
	Email       string   `json:"email"`
	JobTitle    string   `json:"job_title"`
	Timezone    string   `json:"timezone"`
	Language    string   `json:"language"`
	Palette     string   `json:"palette"`
	NotifyEmail bool     `json:"notify_email"`
	MutedKinds  []string `json:"muted_kinds"`
	HasAvatar   bool     `json:"has_avatar"`
}

func updateJiraCurrentUser(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.UpdateCurrentUserArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		// Only what was sent. A mutedKinds list that arrives without the rest
		// of the profile must not blank it, and the endpoint replaces the
		// whole mute list, so the description tells the caller to read first.
		body := map[string]any{}
		if args.DisplayName != nil {
			body["display_name"] = *args.DisplayName
		}
		if args.FirstName != nil {
			body["first_name"] = *args.FirstName
		}
		if args.LastName != nil {
			body["last_name"] = *args.LastName
		}
		if args.Email != nil {
			body["email"] = *args.Email
		}
		if args.JobTitle != nil {
			body["job_title"] = *args.JobTitle
		}
		if args.Timezone != nil {
			body["timezone"] = *args.Timezone
		}
		if args.Language != nil {
			body["language"] = *args.Language
		}
		if args.Palette != nil {
			body["palette"] = *args.Palette
		}
		if args.NotifyEmail != nil {
			body["notify_email"] = *args.NotifyEmail
		}
		if args.MutedKinds != nil {
			body["muted_kinds"] = args.MutedKinds
		}
		if args.Avatar != nil {
			body["avatar"] = *args.Avatar
		}
		if len(body) == 0 {
			return mcp.NewToolResultError(
				"nothing to change: give at least one of displayName, firstName, lastName, " +
					"email, jobTitle, timezone, language, palette, notifyEmail, mutedKinds or avatar"), nil
		}
		// The avatar is logged by presence, not by value: it is a base64 blob
		// and a log line is the wrong place for megabytes.
		logged := map[string]any{}
		for key, value := range body {
			logged[key] = value
		}
		if _, ok := logged["avatar"]; ok {
			logged["avatar"] = "<avatar data URL omitted>"
		}
		log.Printf("[jirrabit-mcp updateJiraCurrentUser] %v", logged)
		var user meDTO
		if err := client.Patch(ctx, "me/", body, &user); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"user": user})
	}
}

type inviteDTO struct {
	ID      int    `json:"id"`
	Token   string `json:"token"`
	RegURL  string `json:"registration_url"`
	Email   string `json:"email"`
	Role    string `json:"role"`
	Valid   bool   `json:"valid"`
	Created string `json:"created_at"`
	Expires string `json:"expires_at"`
	UsedAt  string `json:"used_at"`
	UsedBy  string `json:"used_by"`
}

func listJiraInvites(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListInvitesArgs
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
		items, meta, err := jira.List[inviteDTO](ctx, client, jira.WithPage("admin/invites/", page, size))
		if err != nil {
			return toolError(err)
		}
		// The endpoint already omits the token, but a defensive strip here means
		// the guarantee does not depend on one endpoint's response schema.
		for i := range items {
			items[i].Token = ""
			items[i].RegURL = ""
		}
		return jsonResult(paged{
			Count: meta.Count, StartAt: page, MaxResults: size,
			NextPageToken: jira.NextToken(meta), Values: items,
		})
	}
}

func createJiraInvite(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.CreateInviteArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp createJiraInvite] on %s", client.BaseURL())
		body := map[string]any{"role": orDefault(args.Role, "member")}
		if args.Email != "" {
			body["email"] = args.Email
		}
		if args.Days > 0 {
			body["days"] = args.Days
		}
		var invite inviteDTO
		if err := client.Post(ctx, "admin/invites/", body, &invite); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{
			"invite":          invite,
			"registrationUrl": invite.RegURL,
			"warning":         "the token is shown only in this response; pass it on now and do not log it",
		})
	}
}

func revokeJiraInvite(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.RevokeInviteArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.InviteID <= 0 {
			return mcp.NewToolResultError("inviteId is required, from listJiraInvites"), nil
		}
		log.Printf("[jirrabit-mcp revokeJiraInvite] %d", args.InviteID)
		var out map[string]any
		if err := client.Delete(ctx, fmt.Sprintf("admin/invites/%d/", args.InviteID), &out); err != nil {
			return toolError(err)
		}
		return jsonResult(out)
	}
}

type commentEditDTO struct {
	ID       int    `json:"id"`
	OldBody  string `json:"old_body"`
	EditedAt string `json:"edited_at"`
	EditedBy string `json:"edited_by"`
}

func getJiraCommentHistory(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.GetCommentHistoryArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.IssueIDOrKey == "" || args.CommentID <= 0 {
			return mcp.NewToolResultError("issueIdOrKey and commentId are both required"), nil
		}
		page, err := resolvePage(args.StartAt, args.NextPageToken)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		size := clampSize(args.MaxResults)
		path := fmt.Sprintf("issues/%s/comments/%d/history/",
			url.PathEscape(args.IssueIDOrKey), args.CommentID)
		items, meta, err := jira.List[commentEditDTO](ctx, client, jira.WithPage(path, page, size))
		if err != nil {
			return toolError(err)
		}
		// A map rather than paged, because this one carries two fields the shape
		// does not: the answer has to say it is incomplete, or an agent presents a
		// short list as a full history.
		return jsonResult(map[string]any{
			"total":         meta.Count,
			"startAt":       page,
			"maxResults":    size,
			"nextPageToken": jira.NextToken(meta),
			"values":        items,
			"incomplete":    true,
			"note": "only two jirrabit code paths record a previous comment body, so this is " +
				"not a complete edit history",
		})
	}
}
