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

// registerBulkDataTools wires the two things an agent needs to move a whole set
// of issues at once, and the handful of per-person views that had no endpoint at
// all.
//
// CSV import is the only bulk write in the API an agent can reach, and it is the
// reason the confirmation machinery exists: a CSV of two hundred rows creates two
// hundred issues, and the first call has to show what it read before any of them
// exist.

func registerBulkDataTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("exportJiraIssuesCsv",
		mcp.WithDescription(
			"Export a project's issues as CSV rows, as data rather than as a file.\n\n"+
				"Honours the same filters the issue list does, so \"export what I am "+
				"looking at\" is one call rather than a query built by hand. Archived issues "+
				"are off the board and so off the export unless you ask for them.\n\n"+
				"The columns are key, summary, status, priority, type, assignee, reporter, "+
				"sprint, epic, story_points, estimate_minutes, time_spent_minutes, due_date, "+
				"resolved_at, created_at, updated_at. Narrow the set with `columns` and "+
				"anything unrecognised in it is dropped."),
		mcp.WithTitleAnnotation("Export issues as CSV"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.ExportIssuesCSVArgs](),
	), exportJiraIssuesCsv(d))

	s.AddTool(mcp.NewTool("importJiraIssuesCsv",
		mcp.WithDescription(
			"Create issues in a project from CSV text. Needs edit rights on the project.\n\n"+
				"It is two phases and the first one is the default. Without `dryRun: false` "+
				"the call only parses: it resolves every type, priority and assignee it can, "+
				"reports which rows it would create, which it would skip and why, and names "+
				"the values it could not resolve so a typo is visible. Nothing is created.\n\n"+
				"With `dryRun: false` it creates them, which cannot be undone, so the first "+
				"call returns a preview and a confirmation token. Show the preview to the "+
				"user, wait for their agreement, then call again with the token.\n\n"+
				"Columns: summary (required), description, type, priority, assignee, "+
				"story_points, due_date. Unknown columns are ignored. A value that does not "+
				"resolve falls back to the instance default rather than failing the row, and "+
				"shows up in unknownValues."),
		mcp.WithTitleAnnotation("Import issues from CSV"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.ImportIssuesCSVArgs](),
	), importJiraIssuesCsv(d))

	s.AddTool(mcp.NewTool("listJiraBoardViews",
		mcp.WithDescription(
			"List the caller's saved board views for a project: a name, the board filters it "+
				"applies, whether it is the default, and the board URL it reopens.\n\n"+
				"Views are per-person and per-project, so this never returns somebody else's."),
		mcp.WithTitleAnnotation("List board views"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.ListBoardViewsArgs](),
	), listJiraBoardViews(d))

	s.AddTool(mcp.NewTool("saveJiraBoardView",
		mcp.WithDescription(
			"Save a set of board filters under a name for the caller, in a project.\n\n"+
				"Saving a name that already exists replaces its filters, so this is how to "+
				"edit a view as well as create one. Filter keys are the board's own: assignee, "+
				"type, priority, epic, sprint, stale, due, text — an unrecognised key is "+
				"refused rather than stored, because the board would never read it.\n\n"+
				"Only one view per project can be the default; setting another makes this one it."),
		mcp.WithTitleAnnotation("Save board view"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.SaveBoardViewArgs](),
	), saveJiraBoardView(d))

	s.AddTool(mcp.NewTool("deleteJiraBoardView",
		mcp.WithDescription(
			"Delete one of the caller's saved board views, by id from listJiraBoardViews. The "+
				"issues it matched are untouched — it is a name and a set of filters."),
		mcp.WithTitleAnnotation("Delete board view"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.DeleteBoardViewArgs](),
	), deleteJiraBoardView(d))

	s.AddTool(mcp.NewTool("listJiraRecentIssues",
		mcp.WithDescription(
			"The caller's most recently opened issues, newest first. This is the same list "+
				"the dashboard shows as \"Recientes\".\n\n"+
				"Per person, and filtered to projects the caller can still see — so it never "+
				"returns a key that 404s. An entry whose issue has since been archived says so."),
		mcp.WithTitleAnnotation("List recently viewed issues"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithInputSchema[schema.ListRecentIssuesArgs](),
	), listJiraRecentIssues(d))

	s.AddTool(mcp.NewTool("clearJiraRecentIssues",
		mcp.WithDescription(
			"Empty the caller's \"Recientes\" list. The issues themselves are untouched; only "+
				"the record that they were opened is forgotten. Per person, so it cannot clear "+
				"anybody else's."),
		mcp.WithTitleAnnotation("Clear recently viewed"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.Target](),
	), clearJiraRecentIssues(d))

	s.AddTool(mcp.NewTool("listJiraCommentMentions",
		mcp.WithDescription(
			"Who was @mentioned in a comment, and whether they have since opened the issue.\n\n"+
				"Use it to answer \"did anyone see what I asked\". Naming somebody is already "+
				"public in the comment body, so anyone who can see the issue sees the list; the "+
				"read timestamps are only given to the comment's author and the people "+
				"mentioned, and come back false for anyone else."),
		mcp.WithTitleAnnotation("List comment mentions"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.ListCommentMentionsArgs](),
	), listJiraCommentMentions(d))
}

type csvExportDTO struct {
	Filename string              `json:"filename"`
	Columns  []string            `json:"columns"`
	Rows     []map[string]string `json:"rows"`
	Count    int                 `json:"count"`
}

func exportJiraIssuesCsv(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ExportIssuesCSVArgs
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
		query := url.Values{}
		if args.Columns != "" {
			query.Set("cols", args.Columns)
		}
		if args.Text != "" {
			query.Set("text", args.Text)
		}
		if args.Status > 0 {
			query.Set("status", fmt.Sprint(args.Status))
		}
		if args.Assignee != "" {
			query.Set("assignee", args.Assignee)
		}
		if args.Archived {
			query.Set("archived", "true")
		}
		path := fmt.Sprintf("projects/%s/csv-export/", url.PathEscape(key))
		if encoded := query.Encode(); encoded != "" {
			path += "?" + encoded
		}
		log.Printf("[jirrabit-mcp exportJiraIssuesCsv] %s", key)
		var out csvExportDTO
		if err := client.Get(ctx, path, &out); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{
			"projectKeyOrId": key,
			"filename":       out.Filename,
			"columns":        out.Columns,
			"count":          out.Count,
			"rows":           orEmptyRows(out.Rows),
		})
	}
}

func orEmptyRows(rows []map[string]string) []map[string]string {
	if rows == nil {
		return []map[string]string{}
	}
	return rows
}

type csvImportRowDTO struct {
	Row        int    `json:"row"`
	Summary    string `json:"summary"`
	IssueType  string `json:"issue_type"`
	Priority   string `json:"priority"`
	Assignee   string `json:"assignee"`
	StoryPoint string `json:"story_points"`
	DueDate    string `json:"due_date"`
	Skipped    bool   `json:"skipped"`
	Problem    string `json:"problem"`
}

type csvImportDTO struct {
	DryRun        bool                `json:"dry_run"`
	TotalRows     int                 `json:"total_rows"`
	Creatable     int                 `json:"creatable"`
	Rows          []csvImportRowDTO   `json:"rows"`
	CreatedKeys   []string            `json:"created_keys"`
	UnknownValues map[string][]string `json:"unknown_values"`
}

func importJiraIssuesCsv(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ImportIssuesCSVArgs
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
		if strings.TrimSpace(args.CSV) == "" {
			return mcp.NewToolResultError("csv is required, with a summary column at least"), nil
		}
		// Absent means preview, which is the safe reading of an ambiguous call.
		apply := args.DryRun != nil && !*args.DryRun
		log.Printf("[jirrabit-mcp importJiraIssuesCsv] %s apply=%t", key, apply)

		// A preview is not routed through the confirmation machinery at all: the
		// caller asked to see, not to do, so there is nothing to confirm and no
		// token to hand back. Only the apply path below mints one.
		importPath := fmt.Sprintf("projects/%s/csv-import/", url.PathEscape(key))
		if !apply {
			var parsed csvImportDTO
			if err := client.Post(ctx, importPath, map[string]any{
				"csv": args.CSV, "dry_run": true,
			}, &parsed); err != nil {
				return toolError(err)
			}
			return jsonResult(map[string]any{
				"projectKeyOrId": key,
				"dryRun":         true,
				"totalRows":      parsed.TotalRows,
				"creatable":      parsed.Creatable,
				"rows":           parsed.Rows,
				"unknownValues":  parsed.UnknownValues,
				"next": "nothing was created. To apply, call again with dryRun:false; that " +
					"call will ask for a confirmation first.",
			})
		}

		c := confirmable{
			signer: newConfirmSigner(client),
			op:     "importJiraIssuesCsv",
			target: key,
			build: func() (Preview, func(*jira.Client) (any, error), error) {
				// Parsed here, on purpose. The digest in the confirmation token is
				// taken over these numbers, so what the user agreed to is literally
				// what the apply is described by — not a second parse that could
				// disagree with the first.
				var parsed csvImportDTO
				if err := client.Post(ctx, importPath, map[string]any{
					"csv": args.CSV, "dry_run": true,
				}, &parsed); err != nil {
					return Preview{}, nil, err
				}
				if parsed.Creatable == 0 {
					return Preview{}, nil, fmt.Errorf(
						"el CSV no tiene filas creables: revisa que tenga una columna summary con contenido")
				}
				preview := Preview{
					Operation: "importJiraIssuesCsv",
					Target:    key,
					Summary: fmt.Sprintf("create %d issues in project %s from CSV",
						parsed.Creatable, key),
					Reversible: false,
					Undo:       "",
					Cascade:    []PreviewCascade{{Kind: "issues to be created", Count: parsed.Creatable}},
				}
				if parsed.TotalRows > parsed.Creatable {
					preview.Cascade = append(preview.Cascade, PreviewCascade{
						Kind:   "rows that will be skipped",
						Count:  parsed.TotalRows - parsed.Creatable,
						Detail: csvSkippedDetail(parsed.Rows),
					})
				}
				if len(parsed.UnknownValues) > 0 {
					preview.Summary += fmt.Sprintf("; unresolvable values fall back to defaults: %s",
						csvUnknownDetail(parsed.UnknownValues))
				}
				work := func(cl *jira.Client) (any, error) {
					var applied csvImportDTO
					if err := cl.Post(ctx, importPath, map[string]any{
						"csv": args.CSV, "dry_run": false,
					}, &applied); err != nil {
						return nil, err
					}
					return map[string]any{
						"projectKeyOrId": key,
						"createdKeys":    orEmpty(applied.CreatedKeys),
						"created":        len(applied.CreatedKeys),
					}, nil
				}
				return preview, work, nil
			},
		}
		if !apply {
			// A preview, plainly: no token, no "call me again with this", because
			// the caller did not ask to change anything.
			var parsed csvImportDTO
			path := fmt.Sprintf("projects/%s/csv-import/", url.PathEscape(key))
			if err := client.Post(ctx, path, map[string]any{
				"csv": args.CSV, "dry_run": true,
			}, &parsed); err != nil {
				return toolError(err)
			}
			return jsonResult(map[string]any{
				"projectKeyOrId": key,
				"dryRun":         true,
				"totalRows":      parsed.TotalRows,
				"creatable":      parsed.Creatable,
				"rows":           parsed.Rows,
				"unknownValues":  parsed.UnknownValues,
				"next": "nothing was created. To apply, call again with dryRun:false; that call " +
					"will ask for a confirmation first.",
			})
		}
		return c.run(args.Confirmation.Confirm, client)
	}
}

func csvSkippedDetail(rows []csvImportRowDTO) string {
	var parts []string
	for _, row := range rows {
		if row.Skipped {
			parts = append(parts, fmt.Sprintf("row %d: %s", row.Row, row.Problem))
		}
	}
	if len(parts) > 5 {
		return strings.Join(parts[:5], "; ") + fmt.Sprintf("; and %d more", len(parts)-5)
	}
	return strings.Join(parts, "; ")
}

func csvUnknownDetail(values map[string][]string) string {
	var parts []string
	for field, names := range values {
		parts = append(parts, fmt.Sprintf("%s=%s", field, strings.Join(names, ", ")))
	}
	return strings.Join(parts, "; ")
}

type boardViewDTO struct {
	ID        int               `json:"id"`
	Name      string            `json:"name"`
	Filters   map[string]string `json:"filters"`
	IsDefault bool              `json:"is_default"`
	URL       string            `json:"url"`
	CreatedAt string            `json:"created_at"`
}

func listJiraBoardViews(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListBoardViewsArgs
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
		path := fmt.Sprintf("projects/%s/board-views/", url.PathEscape(key))
		items, meta, err := jira.List[boardViewDTO](ctx, client, jira.WithPage(path, page, size))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count: meta.Count, StartAt: page, MaxResults: size,
			NextPageToken: jira.NextToken(meta), Values: items,
		})
	}
}

func saveJiraBoardView(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.SaveBoardViewArgs
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
			return mcp.NewToolResultError("name is required, e.g. 'Mine, bugs'"), nil
		}
		log.Printf("[jirrabit-mcp saveJiraBoardView] %s %q", key, args.Name)
		var view boardViewDTO
		path := fmt.Sprintf("projects/%s/board-views/", url.PathEscape(key))
		if err := client.Post(ctx, path, map[string]any{
			"name": args.Name, "filters": args.Filters, "is_default": args.IsDefault,
		}, &view); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"boardView": view})
	}
}

func deleteJiraBoardView(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.DeleteBoardViewArgs
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
		if args.ViewID <= 0 {
			return mcp.NewToolResultError("viewId is required, from listJiraBoardViews"), nil
		}
		var out map[string]any
		path := fmt.Sprintf("projects/%s/board-views/%d/", url.PathEscape(key), args.ViewID)
		if err := client.Delete(ctx, path, &out); err != nil {
			return toolError(err)
		}
		return jsonResult(out)
	}
}

type recentVisitDTO struct {
	Issue    string `json:"issue"`
	Summary  string `json:"summary"`
	Status   string `json:"status"`
	ViewedAt string `json:"viewed_at"`
	Archived bool   `json:"archived"`
}

func listJiraRecentIssues(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListRecentIssuesArgs
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
		items, meta, err := jira.List[recentVisitDTO](ctx, client, jira.WithPage("recent/", page, size))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count: meta.Count, StartAt: page, MaxResults: size,
			NextPageToken: jira.NextToken(meta), Values: items,
		})
	}
}

func clearJiraRecentIssues(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.Target
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp clearJiraRecentIssues] on %s", client.BaseURL())
		var out map[string]any
		if err := client.Delete(ctx, "recent/", &out); err != nil {
			return toolError(err)
		}
		return jsonResult(out)
	}
}

type mentionDTO struct {
	Comment   int    `json:"comment"`
	Actor     string `json:"actor"`
	Mentioned string `json:"mentioned"`
	CreatedAt string `json:"created_at"`
	SeenAt    string `json:"seen_at"`
	Seen      bool   `json:"seen"`
}

func listJiraCommentMentions(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListCommentMentionsArgs
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
		var out struct {
			Issue     string       `json:"issueIdOrKey"`
			Comment   int          `json:"commentId"`
			Mentions  []mentionDTO `json:"mentions"`
			Count     int          `json:"count"`
			SeenCount int          `json:"seenCount"`
		}
		path := fmt.Sprintf("issues/%s/comments/%d/mentions/",
			url.PathEscape(args.IssueIDOrKey), args.CommentID)
		if err := client.Get(ctx, path, &out); err != nil {
			return toolError(err)
		}
		return jsonResult(out)
	}
}
