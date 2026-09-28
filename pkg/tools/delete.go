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

// The irreversible tools, and the shape they all share.
//
// Every one of them does nothing on the first call: it reports what would be
// lost and hands back a token, and only acts when that token comes back. See
// confirm.go for why the second step is an instruction the model follows rather
// than a lock, because that is the honest description of what this is.
//
// deleteJiraIssue and deleteJiraProject are the two that cascade widely.
// deleteJiraIssueComment is *not* in this group: jirrabit soft-deletes it and
// restoreJiraIssueComment puts it back with its body intact, so a one-step
// reversible operation should not pretend to need ceremony.

// registerDeleteTools wires the destructive tools. They are only registered when
// JIRRABIT_MCP_ENABLE_DELETE is set, so an agent cannot discover — and therefore
// cannot be tempted by — a permanent-delete capability the operator did not
// offer. jirrabit's own permission checks remain the real gate either way.
func registerDeleteTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("deleteJiraIssue",
		mcp.WithDescription(
			"Delete an issue, in two steps.\n\n"+
				"Call it once with just the key: it changes nothing and returns what the delete would take "+
				"with it — comments, worklogs, attachments, links, subtasks — plus a confirmationToken. Show "+
				"that to the user and get their agreement. Then call it again with confirm set to that token. "+
				"The token is bound to what the first call reported, so if the issue changed in between, the "+
				"second call refuses rather than deleting something you did not look at.\n\n"+
				"To remove an issue without losing any of it, use archiveJiraIssue instead. Only available "+
				"because the operator enabled delete tools."),
		mcp.WithTitleAnnotation("Delete issue"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.DeleteIssueArgs](),
	), deleteJiraIssue(d))

	s.AddTool(mcp.NewTool("deleteJiraProject",
		mcp.WithDescription(
			"Delete a whole Jira space, in two steps.\n\n"+
				"Call it once with just the key: it changes nothing and returns how many issues, members, "+
				"sprints and epics would go, plus a confirmationToken. Get the user's agreement, then call it "+
				"again with that token.\n\n"+
				"This is the widest cascade in jirrabit: every issue and therefore every comment, worklog, "+
				"attachment, link and history row, plus the members, sprints, epics, webhooks, custom fields, "+
				"wiki and issue templates. The audit trail is destroyed with it and cannot be recovered, and "+
				"recreating the same project key afterwards will collide with the issues that already exist. "+
				"Archive it with updateJiraProject's archived instead unless the project is genuinely finished. "+
				"Only available because the operator enabled delete tools."),
		mcp.WithTitleAnnotation("Delete project"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.DeleteProjectArgs](),
	), deleteJiraProject(d))

	s.AddTool(mcp.NewTool("deleteJiraIssueWorklog",
		mcp.WithDescription(
			"Delete one logged-time entry, in two steps. The minutes are subtracted from the issue's "+
				"timeSpentSeconds and its remaining estimate, and there is no way to put them back. "+
				"Call once to preview, then again with the confirmationToken."),
		mcp.WithTitleAnnotation("Delete worklog"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.DeleteWorkLogArgs](),
	), deleteJiraIssueWorklog(d))

	s.AddTool(mcp.NewTool("deleteJiraIssueLink",
		mcp.WithDescription(
			"Remove the link between two issues, in two steps. The link is not recoverable. "+
				"Call once to preview, then again with the confirmationToken."),
		mcp.WithTitleAnnotation("Delete issue link"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.DeleteLinkArgs](),
	), deleteJiraIssueLink(d))
}

// jiraIssueImpact mirrors jirrabit's DeletionImpactOut. Only the fields a
// preview needs; the endpoint reports a few more.
type jiraIssueImpact struct {
	Key         string `json:"key"`
	Summary     string `json:"summary"`
	Project     string `json:"project"`
	Status      string `json:"status"`
	Comments    int    `json:"comments"`
	Worklogs    int    `json:"worklogs"`
	Attachments int    `json:"attachments"`
	LinksOut    int    `json:"links_out"`
	LinksIn     int    `json:"links_in"`
	Subtasks    int    `json:"subtasks"`
	Watchers    int    `json:"watchers"`
	Minutes     int    `json:"minutes_logged"`
	Total       int    `json:"total"`
}

func deleteJiraIssue(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.DeleteIssueArgs
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
		key := args.IssueIDOrKey
		log.Printf("[jirrabit-mcp deleteJiraIssue] %s on %s", key, client.BaseURL())

		c := confirmable{
			signer: newConfirmSigner(client),
			op:     "deleteJiraIssue",
			target: key,
			build: func() (Preview, func(*jira.Client) (any, error), error) {
				var impact jiraIssueImpact
				if err := client.Get(
					ctx, fmt.Sprintf("issues/%s/deletion-impact/", url.PathEscape(key)), &impact,
				); err != nil {
					return Preview{}, nil, err
				}
				preview := Preview{
					Operation: "deleteJiraIssue",
					Target:    key,
					Summary: fmt.Sprintf(
						"%s %q (%s, %s)", key, impact.Summary, impact.Project, impact.Status),
					Cascade:     issueCascade(impact),
					Reversible:  false,
					Alternative: "archiveJiraIssue, which hides the issue and can be undone",
				}
				work := func(cl *jira.Client) (any, error) {
					// Not retried: a lost response would turn a delete that worked
					// into a 404, and the agent would believe it failed.
					if err := cl.DeleteOnce(ctx, fmt.Sprintf("issues/%s/", url.PathEscape(key)), nil); err != nil {
						return nil, err
					}
					return map[string]any{"deleted": true, "key": key}, nil
				}
				return preview, work, nil
			},
		}
		return c.run(args.Confirmation.Confirm, client)
	}
}

// issueCascade renders the impact as rows a person can read, dropping the ones
// that are zero. A list of eight zeroes tells the reader less than a list of the
// two things that actually exist.
func issueCascade(impact jiraIssueImpact) []PreviewCascade {
	rows := []PreviewCascade{
		{Kind: "comments", Count: impact.Comments},
		{Kind: "worklogs", Count: impact.Worklogs,
			Detail: pluralMinutes(impact.Minutes)},
		{Kind: "attachments", Count: impact.Attachments},
		{Kind: "issue links (this side)", Count: impact.LinksOut},
		{Kind: "issue links (other side)", Count: impact.LinksIn},
		{Kind: "subtasks, deleted with their parent", Count: impact.Subtasks},
		{Kind: "watchers, who stop being notified", Count: impact.Watchers},
	}
	kept := rows[:0]
	for _, row := range rows {
		if row.Count > 0 {
			kept = append(kept, row)
		}
	}
	return kept
}

func pluralMinutes(minutes int) string {
	if minutes == 0 {
		return ""
	}
	hours := minutes / 60
	rest := minutes % 60
	switch {
	case hours == 0:
		return fmt.Sprintf("%d min logged", rest)
	case rest == 0:
		return fmt.Sprintf("%dh logged", hours)
	default:
		return fmt.Sprintf("%dh%02dm logged", hours, rest)
	}
}

// jiraProjectImpact mirrors jirrabit's ProjectDeletionImpactOut.
type jiraProjectImpact struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Archived bool   `json:"archived"`
	Issues   int    `json:"issues"`
	Members  int    `json:"members"`
	Sprints  int    `json:"sprints"`
	Epics    int    `json:"epics"`
}

func deleteJiraProject(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.DeleteProjectArgs
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
		log.Printf("[jirrabit-mcp deleteJiraProject] %s on %s", key, client.BaseURL())

		c := confirmable{
			signer: newConfirmSigner(client),
			op:     "deleteJiraProject",
			target: key,
			build: func() (Preview, func(*jira.Client) (any, error), error) {
				var impact jiraProjectImpact
				if err := client.Get(
					ctx, fmt.Sprintf("projects/%s/deletion-impact/", url.PathEscape(key)), &impact,
				); err != nil {
					return Preview{}, nil, err
				}
				preview := Preview{
					Operation: "deleteJiraProject",
					Target:    key,
					Summary: fmt.Sprintf(
						"the whole project %s %q: %d issues and everything hanging off them",
						key, impact.Name, impact.Issues),
					Cascade: []PreviewCascade{
						{Kind: "issues", Count: impact.Issues,
							Detail: "and every comment, worklog, attachment, link and history row on them"},
						{Kind: "members", Count: impact.Members},
						{Kind: "sprints", Count: impact.Sprints},
						{Kind: "epics", Count: impact.Epics},
					},
					Reversible: false,
					// Not just "you lose the data": the audit table is a child of
					// the project, so the record that it was deleted is deleted
					// too, and the keys are globally unique so recreating the
					// project collides with what is left.
					Alternative: "updateJiraProject with archived: true, which keeps the data and can be undone",
				}
				if impact.Archived {
					// Archived already means the project is out of the way, which
					// is the usual reason to want it gone.
					preview.Alternative = "this project is already archived; deleting it is rarely what anyone wants"
				}
				work := func(cl *jira.Client) (any, error) {
					if err := cl.DeleteOnce(ctx, fmt.Sprintf("projects/%s/", url.PathEscape(key)), nil); err != nil {
						return nil, err
					}
					return map[string]any{"deleted": true, "projectKey": key}, nil
				}
				return preview, work, nil
			},
		}
		return c.run(args.Confirmation.Confirm, client)
	}
}

func deleteJiraIssueWorklog(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.DeleteWorkLogArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		if args.WorklogID == "" {
			return mcp.NewToolResultError("worklogId is required, from listJiraIssueWorklogs"), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		target := args.IssueIDOrKey + "/" + args.WorklogID
		log.Printf("[jirrabit-mcp deleteJiraIssueWorklog] %s on %s", target, client.BaseURL())

		c := confirmable{
			signer: newConfirmSigner(client),
			op:     "deleteJiraIssueWorklog",
			target: target,
			build: func() (Preview, func(*jira.Client) (any, error), error) {
				shaper := d.Pool.ShaperFor(client)
				log, err := findWorklog(ctx, client, shaper, args.IssueIDOrKey, args.WorklogID)
				if err != nil {
					return Preview{}, nil, err
				}
				preview := Preview{
					Operation: "deleteJiraIssueWorklog",
					Target:    target,
					Summary: fmt.Sprintf(
						"%s of time logged on %s by %s, commented %q",
						log.Duration, args.IssueIDOrKey, log.Author, truncateForPreview(log.Comment)),
					Cascade: []PreviewCascade{{
						Kind:  "the issue's time totals",
						Count: 1,
						Detail: fmt.Sprintf(
							"timeSpentSeconds drops by %d and the remaining estimate goes back up by the same "+
								"amount, so the two stop agreeing with the entries behind them", log.TimeSpentSeconds),
					}},
					Reversible: false,
				}
				work := func(cl *jira.Client) (any, error) {
					var out jira.WorkLog
					path := fmt.Sprintf("issues/%s/worklogs/%s/",
						url.PathEscape(args.IssueIDOrKey), url.PathEscape(args.WorklogID))
					if err := cl.DeleteOnce(ctx, path, &out); err != nil {
						return nil, err
					}
					return map[string]any{"deleted": true, "minutes": out.Minutes}, nil
				}
				return preview, work, nil
			},
		}
		return c.run(args.Confirmation.Confirm, client)
	}
}

func deleteJiraIssueLink(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.DeleteLinkArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		if args.LinkID == "" {
			return mcp.NewToolResultError("linkId is required, from getJiraIssueLinks"), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		target := args.IssueIDOrKey + "/" + args.LinkID
		log.Printf("[jirrabit-mcp deleteJiraIssueLink] %s on %s", target, client.BaseURL())

		c := confirmable{
			signer: newConfirmSigner(client),
			op:     "deleteJiraIssueLink",
			target: target,
			build: func() (Preview, func(*jira.Client) (any, error), error) {
				link, err := findLink(ctx, client, args.IssueIDOrKey, args.LinkID)
				if err != nil {
					return Preview{}, nil, err
				}
				preview := Preview{
					Operation: "deleteJiraIssueLink",
					Target:    target,
					Summary: fmt.Sprintf(
						"the %q link between %s and %s", link.Type, args.IssueIDOrKey, link.OtherIssue),
					Cascade: []PreviewCascade{{
						Kind:   "the link and, if jirrabit stored one, its inverse",
						Count:  1,
						Detail: "nothing else: both issues and their history stay",
					}},
					Reversible: false,
				}
				work := func(cl *jira.Client) (any, error) {
					path := fmt.Sprintf("issues/%s/links/%s/",
						url.PathEscape(args.IssueIDOrKey), url.PathEscape(args.LinkID))
					if err := cl.DeleteOnce(ctx, path, nil); err != nil {
						return nil, err
					}
					return map[string]any{"deleted": true, "linkId": args.LinkID}, nil
				}
				return preview, work, nil
			},
		}
		return c.run(args.Confirmation.Confirm, client)
	}
}

func truncateForPreview(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 60 {
		if s == "" {
			return ""
		}
		return s
	}
	return s[:57] + "…"
}

// foundWorklog is what a preview needs from one worklog entry.
//
// The duration and seconds count come from the shaper rather than being computed
// here, so the preview quotes exactly the numbers listJiraIssueWorklogs shows —
// a preview that formatted time differently from the tool the user just read
// would be a small lie in the one place that must not lie.
type foundWorklog struct {
	ID               int
	Author           string
	Minutes          int
	Comment          string
	LoggedAt         string
	Duration         string
	TimeSpentSeconds int
}

func findWorklog(
	ctx context.Context, client *jira.Client, shaper *jira.Shaper, issueKey, worklogID string,
) (foundWorklog, error) {
	path := fmt.Sprintf("issues/%s/worklogs/?size=200", url.PathEscape(issueKey))
	rows, _, err := jira.List[jira.WorkLog](ctx, client, path)
	if err != nil {
		return foundWorklog{}, err
	}
	for _, row := range rows {
		if fmt.Sprint(row.ID) != worklogID {
			continue
		}
		shaped := shaper.WorkLog(row)
		return foundWorklog{
			ID:               row.ID,
			Author:           row.Author,
			Minutes:          row.Minutes,
			Comment:          row.Comment,
			LoggedAt:         row.LoggedAt,
			Duration:         shaped.TimeSpent,
			TimeSpentSeconds: shaped.TimeSpentSeconds,
		}, nil
	}
	return foundWorklog{}, &jira.APIError{
		StatusCode: 404,
		Method:     "GET",
		Path:       path,
		Detail: fmt.Sprintf("worklog %s is not on %s. Call listJiraIssueWorklogs for the ids that are",
			worklogID, issueKey),
	}
}

// foundLink is the preview's view of one link: the other end, because "delete
// the link" means nothing to a person who cannot see what it connects.
type foundLink struct {
	ID         int    `json:"id"`
	Type       string `json:"type"`
	OtherIssue string
}

func findLink(ctx context.Context, client *jira.Client, issueKey, linkID string) (foundLink, error) {
	path := fmt.Sprintf("issues/%s/links/", url.PathEscape(issueKey))
	var raw []struct {
		ID      int    `json:"id"`
		Type    string `json:"type"`
		Source  string `json:"source"`
		Target  string `json:"target"`
		Outward string `json:"outward_issue_key"`
		Inward  string `json:"inward_issue_key"`
	}
	if err := client.Get(ctx, path, &raw); err != nil {
		return foundLink{}, err
	}
	for _, row := range raw {
		if fmt.Sprint(row.ID) == linkID {
			// jirrabit's LinkOut names both ends as keys, with `source` being
			// the issue the link hangs off. Whichever end is not the one asked
			// about is the other end.
			other := row.Target
			if other == "" || other == issueKey {
				other = row.Source
			}
			return foundLink{ID: row.ID, Type: row.Type, OtherIssue: other}, nil
		}
	}
	return foundLink{}, &jira.APIError{
		StatusCode: 404,
		Method:     "GET",
		Path:       path,
		Detail: fmt.Sprintf("link %s is not on %s. Call getJiraIssueLinks for the ids that are",
			linkID, issueKey),
	}
}
