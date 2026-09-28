package tools

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/rroblf01/jirrabit-mcp/pkg/jira"
	"github.com/rroblf01/jirrabit-mcp/pkg/schema"
)

// Reading the past, and the account surface.
//
// An agent can change an issue and cannot answer "what changed, who changed it,
// or when" — which is the question behind most reporting and most incident
// triage. jirrabit keeps two logs and they are not interchangeable, so the tools
// are kept apart and each description says which one it is: the changelog is
// field-level and incomplete, the activity feed is signal-driven and complete
// but has no before/after.

// --- changelog and activity ------------------------------------------------

type changelogEntry struct {
	ID        int    `json:"id"`
	Field     string `json:"field"`
	OldValue  string `json:"old_value"`
	NewValue  string `json:"new_value"`
	Actor     string `json:"actor"`
	CreatedAt string `json:"created_at"`
}

type activityEntry struct {
	ID          int            `json:"id"`
	Verb        string         `json:"verb"`
	TargetType  string         `json:"target_type"`
	TargetID    *int           `json:"target_id"`
	TargetLabel string         `json:"target_label"`
	Actor       string         `json:"actor"`
	CreatedAt   string         `json:"created_at"`
	Metadata    map[string]any `json:"metadata"`
}

func registerHistoryTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("getJiraIssueChangelog",
		mcp.WithDescription(
			"What changed on one issue, field by field, newest first. Optional `field` narrows it to a "+
				"single one, which is the honest way to ask \"when did the status last change\".\n\n"+
				"This log is INCOMPLETE. jirrabit writes a history row from only two places — a status "+
				"transition and a worklog entry — so a summary or assignee edit leaves no row here. Read "+
				"silence as \"this log does not cover that\", never as \"nothing changed\". For who touched "+
				"an issue and when, with no gaps, use getJiraProjectActivity instead."),
		mcp.WithTitleAnnotation("Get issue changelog"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.ChangelogArgs](),
	), getJiraIssueChangelog(d))

	s.AddTool(mcp.NewTool("getJiraProjectActivity",
		mcp.WithDescription(
			"A project's activity feed: creates, updates and deletes, with the actor and a timestamp. "+
				"`verb` narrows it to one kind, e.g. 'deleted'.\n\n"+
				"This is the complete record of *that* something happened, because it comes from signals "+
				"rather than from individual call sites. It carries no before/after values, so for \"what "+
				"did this say before\", use getJiraIssueChangelog. Rows are dropped by the purge_old_data "+
				"cron after 90 days by default, so this is recent history and not an archive."),
		mcp.WithTitleAnnotation("Get project activity"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.ProjectActivityArgs](),
	), getJiraProjectActivity(d))

	s.AddTool(mcp.NewTool("getJiraProjectSla",
		mcp.WithDescription(
			"Open issues stuck in one status longer than a threshold, oldest first. "+
				"\"Time at current status\" is the newest status-change row, or the issue's "+
				"own creation when it never moved; done-category issues are never stuck.\n\n"+
				"Use it to answer \"what is blocked\" without downloading every changelog. "+
				"Days defaults to 7 and clamps to a minimum of 1."),
		mcp.WithTitleAnnotation("Get project SLA breaches"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.ProjectSlaArgs](),
	), getJiraProjectSla(d))

	s.AddTool(mcp.NewTool("getJiraProjectBurndown",
		mcp.WithDescription(
			"A sprint's burndown in story points plus recent velocity: committed vs "+
				"completed per closed sprint, and the ideal line against what actually "+
				"remains, day by day.\n\n"+
				"Future days carry no actual rather than zero — a client that plots null "+
				"as zero draws a cliff that is not there. Omit the sprint for the active "+
				"one, else the latest by start date; a project with no sprints answers "+
				"empty rather than 404."),
		mcp.WithTitleAnnotation("Get project burndown"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.ProjectBurndownArgs](),
	), getJiraProjectBurndown(d))

	s.AddTool(mcp.NewTool("getJiraProjectReports",
		mcp.WithDescription(
			"Throughput per ISO week for the last eight weeks, cycle-time stats over "+
				"issues resolved in the last ninety days, and a WIP-by-status snapshot.\n\n"+
				"Two caveats travel with the numbers. Cycle time starts at the first "+
				"recorded status change, falling back to the issue's creation — and like "+
				"every changelog built on HistoryEntry that history is incomplete, so "+
				"cycle time is a lower bound as much as a measurement. And WIP counts "+
				"every issue in each status, archived included, exactly as the web page "+
				"does."),
		mcp.WithTitleAnnotation("Get project reports"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.ProjectReportsArgs](),
	), getJiraProjectReports(d))
}

type slaItem struct {
	Issue        string `json:"issue"`
	Summary      string `json:"summary"`
	Status       string `json:"status"`
	Priority     string `json:"priority"`
	Assignee     string `json:"assignee"`
	EnteredAt    string `json:"entered_at"`
	DaysInStatus int    `json:"days_in_status"`
}

type slaReport struct {
	Project       string    `json:"project"`
	ThresholdDays int       `json:"threshold_days"`
	Count         int       `json:"count"`
	Items         []slaItem `json:"items"`
}

func getJiraProjectSla(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ProjectSlaArgs
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
		path := fmt.Sprintf("projects/%s/sla/", url.PathEscape(key))
		if args.Days > 0 {
			path += "?days=" + strconv.Itoa(args.Days)
		}
		log.Printf("[jirrabit-mcp getJiraProjectSla] %s", key)
		var out slaReport
		if err := client.Get(ctx, path, &out); err != nil {
			return toolError(err)
		}
		if out.Items == nil {
			out.Items = []slaItem{}
		}
		return jsonResult(out)
	}
}

type burndownPoint struct {
	Date   string   `json:"date"`
	Ideal  float64  `json:"ideal"`
	Actual *float64 `json:"actual"`
}

type burndownSprint struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

type velocityRow struct {
	Name      string `json:"name"`
	Committed int    `json:"committed"`
	Completed int    `json:"completed"`
}

type burndownReport struct {
	Project     string          `json:"project"`
	Sprint      *burndownSprint `json:"sprint"`
	TotalSP     int             `json:"total_sp"`
	DoneSP      int             `json:"done_sp"`
	PercentDone float64         `json:"percent_done"`
	Points      []burndownPoint `json:"points"`
	Velocity    []velocityRow   `json:"velocity"`
}

func getJiraProjectBurndown(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ProjectBurndownArgs
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
		path := fmt.Sprintf("projects/%s/burndown/", url.PathEscape(key))
		if args.SprintID != nil {
			path += "?sprint=" + strconv.Itoa(*args.SprintID)
		}
		log.Printf("[jirrabit-mcp getJiraProjectBurndown] %s", key)
		var out burndownReport
		if err := client.Get(ctx, path, &out); err != nil {
			return toolError(err)
		}
		if out.Points == nil {
			out.Points = []burndownPoint{}
		}
		if out.Velocity == nil {
			out.Velocity = []velocityRow{}
		}
		return jsonResult(out)
	}
}

type throughputWeek struct {
	Week  string `json:"week"`
	Count int    `json:"count"`
}

type cycleTime struct {
	Count   int     `json:"count"`
	MedianH float64 `json:"median_h"`
	AvgH    float64 `json:"avg_h"`
	P90H    float64 `json:"p90_h"`
}

type wipRow struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Count    int    `json:"count"`
}

type reportsReport struct {
	Project       string           `json:"project"`
	Throughput    []throughputWeek `json:"throughput"`
	ThroughputMax int              `json:"throughput_max"`
	Cycle         cycleTime        `json:"cycle"`
	WIP           []wipRow         `json:"wip"`
	WIPMax        int              `json:"wip_max"`
}

func getJiraProjectReports(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ProjectReportsArgs
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
		log.Printf("[jirrabit-mcp getJiraProjectReports] %s", key)
		var out reportsReport
		path := fmt.Sprintf("projects/%s/reports/", url.PathEscape(key))
		if err := client.Get(ctx, path, &out); err != nil {
			return toolError(err)
		}
		if out.Throughput == nil {
			out.Throughput = []throughputWeek{}
		}
		if out.WIP == nil {
			out.WIP = []wipRow{}
		}
		return jsonResult(out)
	}
}

func getJiraIssueChangelog(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ChangelogArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		page, err := resolvePage(args.StartAt, args.NextPageToken)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		size := clampSize(args.MaxResults)
		log.Printf("[jirrabit-mcp getJiraIssueChangelog] %s", args.IssueIDOrKey)

		path := fmt.Sprintf("issues/%s/changelog/", url.PathEscape(args.IssueIDOrKey))
		if args.Field != "" {
			path += "?field=" + url.QueryEscape(args.Field)
		}
		rows, meta, err := jira.List[changelogEntry](ctx, client, jira.WithPage(path, page, size))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count: meta.Count, StartAt: page, MaxResults: size,
			NextPageToken: jira.NextToken(meta), Values: rows,
		})
	}
}

func getJiraProjectActivity(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ProjectActivityArgs
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
		page, err := resolvePage(args.StartAt, args.NextPageToken)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		size := clampSize(args.MaxResults)
		log.Printf("[jirrabit-mcp getJiraProjectActivity] %s", key)

		path := fmt.Sprintf("projects/%s/activity/", url.PathEscape(key))
		if args.Verb != "" {
			path += "?verb=" + url.QueryEscape(args.Verb)
		}
		rows, meta, err := jira.List[activityEntry](ctx, client, jira.WithPage(path, page, size))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count: meta.Count, StartAt: page, MaxResults: size,
			NextPageToken: jira.NextToken(meta), Values: rows,
		})
	}
}

// --- attachments -----------------------------------------------------------

type attachmentDTO struct {
	ID          int    `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
	UploadedBy  string `json:"uploaded_by"`
	UploadedAt  string `json:"uploaded_at"`
	// DataURL is omitempty so a listing does not report `"dataUrl": ""` on every
	// row. An empty string is not the same claim as an absent field: the first
	// says the file has no contents, the second says they were not asked for.
	// This is the same mistake the shaper used to make with `components`.
	DataURL string `json:"dataUrl,omitempty"`
}

func registerAttachmentTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("listJiraIssueAttachments",
		mcp.WithDescription(
			"List the files attached to an issue, with their size and content type. The bytes are not in "+
				"this response — getJiraAttachment fetches one — because a page of them would be megabytes of "+
				"base64 to answer \"is there a screenshot on this\"."),
		mcp.WithTitleAnnotation("List attachments"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.ListAttachmentsArgs](),
	), listJiraIssueAttachments(d))

	s.AddTool(mcp.NewTool("getJiraAttachment",
		mcp.WithDescription(
			"Fetch one attachment. The body carries a `dataUrl` — a data: URL holding the base64 contents — "+
				"which is the same shape the web UI uses for a download link. jirrabit stores files in the "+
				"database, not on a filesystem, so there is no file path to open."),
		mcp.WithTitleAnnotation("Get attachment"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.GetAttachmentArgs](),
	), getJiraAttachment(d))

	s.AddTool(mcp.NewTool("addJiraAttachment",
		mcp.WithDescription(
			"Attach a file to an issue. `data` is the base64 of the file with no data: prefix, at most 5 MB "+
				"of raw bytes, which is about 6.7 MB once encoded. There is no path to read from: the caller "+
				"has to encode the bytes itself, because a JSON call cannot carry a file."),
		mcp.WithTitleAnnotation("Add attachment"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.AddAttachmentArgs](),
	), addJiraAttachment(d))
}

func listJiraIssueAttachments(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListAttachmentsArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		page, err := resolvePage(args.StartAt, args.NextPageToken)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		size := clampSize(args.MaxResults)
		log.Printf("[jirrabit-mcp listJiraIssueAttachments] %s", args.IssueIDOrKey)
		path := fmt.Sprintf("issues/%s/attachments/", url.PathEscape(args.IssueIDOrKey))
		rows, meta, err := jira.List[attachmentDTO](ctx, client, jira.WithPage(path, page, size))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count: meta.Count, StartAt: page, MaxResults: size,
			NextPageToken: jira.NextToken(meta), Values: rows,
		})
	}
}

func getJiraAttachment(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.GetAttachmentArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.AttachmentID == 0 {
			return mcp.NewToolResultError("attachmentId is required, from listJiraIssueAttachments"), nil
		}
		log.Printf("[jirrabit-mcp getJiraAttachment] %d", args.AttachmentID)
		var out attachmentDTO
		if err := client.Get(ctx, fmt.Sprintf("attachments/%d/", args.AttachmentID), &out); err != nil {
			return toolError(err)
		}
		return jsonResult(out)
	}
}

func addJiraAttachment(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.AddAttachmentArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		if strings.TrimSpace(args.Filename) == "" {
			return mcp.NewToolResultError("filename is required"), nil
		}
		if strings.TrimSpace(args.Data) == "" {
			return mcp.NewToolResultError(
				"data is required: the base64 of the file, with no data: prefix"), nil
		}
		// Checked here as well as on the server, because the failure is expensive
		// to discover at the other end: a 5 MB file is 6.7 MB of base64 to push
		// over the wire and back to learn it is too big.
		if decoded, err := base64DecodedLen(args.Data); err != nil {
			return mcp.NewToolResultErrorf("data is not valid base64: %v", err), nil
		} else if decoded > 5*1024*1024 {
			return mcp.NewToolResultErrorf(
				"the file is %d bytes and the maximum is %d (about 6.7 MB once encoded). "+
					"jirrabit stores attachments in the database, not on disk", decoded, 5*1024*1024), nil
		}
		log.Printf("[jirrabit-mcp addJiraAttachment] %s on %s", args.Filename, client.BaseURL())
		body := map[string]any{"filename": args.Filename, "data": args.Data}
		if args.ContentType != "" {
			body["content_type"] = args.ContentType
		}
		var out attachmentDTO
		path := fmt.Sprintf("issues/%s/attachments/", url.PathEscape(args.IssueIDOrKey))
		if err := client.Post(ctx, path, body, &out); err != nil {
			return toolError(err)
		}
		return jsonResult(out)
	}
}

// --- notifications ---------------------------------------------------------

type notificationDTO struct {
	ID        int    `json:"id"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	URL       string `json:"url"`
	Read      bool   `json:"read"`
	Actor     string `json:"actor"`
	CreatedAt string `json:"created_at"`
}

func registerNotificationTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("listJiraNotifications",
		mcp.WithDescription(
			"The API key owner's own notifications: mentions, assignments, comments, status changes. "+
				"Pass unreadOnly to get just the ones not yet seen. Never anybody else's — this is scoped "+
				"to the key, not to a user argument."),
		mcp.WithTitleAnnotation("List notifications"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.ListNotificationsArgs](),
	), listJiraNotifications(d))

	s.AddTool(mcp.NewTool("markJiraNotificationsRead",
		mcp.WithDescription(
			"Mark notifications read. Omit ids to mark all of them. Ids belonging to anyone else are "+
				"ignored rather than applied."),
		mcp.WithTitleAnnotation("Mark notifications read"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.MarkReadArgs](),
	), markJiraNotificationsRead(d))
}

func listJiraNotifications(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListNotificationsArgs
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
		log.Printf("[jirrabit-mcp listJiraNotifications] %s", client.BaseURL())
		path := "notifications/"
		if args.UnreadOnly {
			path += "?unread_only=true"
		}
		rows, meta, err := jira.List[notificationDTO](ctx, client, jira.WithPage(path, page, size))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count: meta.Count, StartAt: page, MaxResults: size,
			NextPageToken: jira.NextToken(meta), Values: rows,
		})
	}
}

func markJiraNotificationsRead(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.MarkReadArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp markJiraNotificationsRead] %d ids", len(args.IDs))
		var out struct {
			MarkedRead int `json:"marked_read"`
		}
		if err := client.Post(ctx, "notifications/read/", args, &out); err != nil {
			return toolError(err)
		}
		return jsonResult(out)
	}
}

// --- teams -----------------------------------------------------------------

type teamDTO struct {
	ID          int      `json:"id"`
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Members     []string `json:"members"`
	CreatedAt   string   `json:"created_at"`
}

func registerTeamTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("listJiraTeams",
		mcp.WithDescription(
			"List the teams on the instance, with their member usernames. A team is what a `@team:slug` "+
				"mention in a comment resolves to, so this is how to find out who such a mention reaches. "+
				"Anyone may read it; writing one needs a superuser."),
		mcp.WithTitleAnnotation("List teams"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.ListLinkTypesArgs](),
	), listJiraTeams(d))
}

func listJiraTeams(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		client, err := d.clientFor(ctx, req)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp listJiraTeams] %s", client.BaseURL())
		rows, meta, err := jira.List[teamDTO](ctx, client, jira.WithPage("teams/", 1, 200))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count: meta.Count, StartAt: 1, MaxResults: 200,
			NextPageToken: jira.NextToken(meta), Values: rows,
		})
	}
}
