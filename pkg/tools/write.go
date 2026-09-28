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

// registerIssueWriteTools wires the mutating tools that are available against
// jirrabit's current REST surface.
func registerIssueWriteTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("createJiraIssue",
		mcp.WithDescription("Create a new Jira work item."),
		mcp.WithTitleAnnotation("Create issue"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.CreateIssueArgs](),
	), createJiraIssue(d))

	s.AddTool(mcp.NewTool("editJiraIssue",
		mcp.WithDescription("Edit an existing work item; only fields you pass are changed. To change status, use transitionJiraIssue — jirrabit enforces its workflow there."),
		mcp.WithTitleAnnotation("Edit issue"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.EditIssueArgs](),
	), editJiraIssue(d))

	s.AddTool(mcp.NewTool("transitionJiraIssue",
		mcp.WithDescription("Move an issue to another status. jirrabit enforces its workflow: a transition the project's configuration forbids is rejected, not forced. Pass statusId from listJiraStatuses, or statusName to look it up by name."),
		mcp.WithTitleAnnotation("Transition issue"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.TransitionIssueArgs](),
	), transitionJiraIssue(d))

	s.AddTool(mcp.NewTool("addOrEditJiraIssueComment",
		mcp.WithDescription("Add a comment, or edit an existing one. Pass commentId to replace an existing comment's body; omit it to create a new comment."),
		mcp.WithTitleAnnotation("Add or edit comment"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.AddOrEditCommentArgs](),
	), addOrEditJiraIssueComment(d))

	s.AddTool(mcp.NewTool("addOrEditJiraIssueWorklog",
		mcp.WithDescription("Log time on a work item, or edit an existing worklog. Pass worklogId to edit; omit it to log new time."),
		mcp.WithTitleAnnotation("Add or edit worklog"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.AddOrEditWorkLogArgs](),
	), addOrEditJiraIssueWorklog(d))

	s.AddTool(mcp.NewTool("archiveJiraIssue",
		mcp.WithDescription(
			"Archive an issue, or bring a archived one back. Archiving hides it from the project list, from "+
				"search and from JQL, and keeps everything: comments, worklogs, links, history. Prefer it to "+
				"deleteJiraIssue whenever the work is finished rather than mistaken — it is one call instead of "+
				"two, and it can be undone by passing archived: false."),
		mcp.WithTitleAnnotation("Archive or unarchive issue"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.ArchiveIssueArgs](),
	), archiveJiraIssue(d))
}

func createJiraIssue(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.CreateIssueArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Argument validation happens before the instance is resolved: a bad
		// argument is a client mistake, and reporting it as a connection
		// problem sends the caller looking in the wrong place.
		// Only the keys that have always been named arguments count as
		// duplicates. priority_id, status_id and sprint_id now have named
		// arguments too, but they were reachable through `fields` before that and
		// an agent using the older spelling should keep working; the named
		// argument simply wins if both are sent.
		extra, err := normaliseFields(args.Fields, createProvidedNames(args))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if args.ProjectKey == "" {
			return mcp.NewToolResultError("projectKey is required, e.g. WEB"), nil
		}
		if args.Summary == "" {
			return mcp.NewToolResultError("summary is required"), nil
		}
		client, shaper, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp createJiraIssue] %s %q", args.ProjectKey, args.Summary)

		// Built as a map rather than a struct so the free-form `fields` object
		// can be merged in, and so a key that was not passed is simply absent:
		// for a PATCH, "not sent" and "sent as empty" are different requests.
		payload := map[string]any{"summary": args.Summary}
		if args.Description != "" {
			payload["description"] = args.Description
		}
		if args.StoryPoints != nil {
			payload["story_points"] = *args.StoryPoints
		}
		if args.DueDate != "" {
			payload["due_date"] = args.DueDate
		}
		id, err := resolveIssueType(ctx, client, args.IssueTypeName, args.IssueTypeID)
		if err != nil {
			return toolError(err)
		}
		if id != nil {
			payload["issue_type_id"] = *id
		}
		if args.Assignee != "" {
			assignee, err := resolveAssignee(ctx, client, args.ProjectKey, args.Assignee)
			if err != nil {
				return toolError(err)
			}
			payload["assignee_id"] = assignee
		}
		// A named argument wins over the same key inside `fields`: it was the
		// more specific thing the caller said.
		for key, value := range map[string]*int{
			"priority_id":      args.PriorityID,
			"status_id":        args.StatusID,
			"sprint_id":        args.SprintID,
			"epic_id":          args.EpicID,
			"estimate_minutes": args.EstimateMinutes,
		} {
			if value != nil {
				payload[key] = *value
			}
		}
		// A key, not an id, because that is what the caller has: the argument is
		// named `parent` for the same reason the others take keys.
		if args.Parent != "" {
			payload["parent"] = args.Parent
		}
		for key, value := range extra {
			payload[key] = value
		}

		var issue jira.Issue
		path := fmt.Sprintf("projects/%s/issues/", url.PathEscape(args.ProjectKey))
		if err := client.Post(ctx, path, payload, &issue); err != nil {
			return toolError(err)
		}
		return jsonResult(shaper.Issue(issue))
	}
}

func editJiraIssue(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.EditIssueArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Validated before the instance is resolved, so an argument mistake is
		// reported as an argument mistake.
		// As on create: the long-standing named arguments are duplicates, the
		// newly-named ids are accepted here and overridden if also given
		// directly.
		extra, err := normaliseFields(args.Fields, editProvidedNames(args))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if args.Assignee != nil && *args.Assignee == "" {
			// jirrabit has no unassign convention in the API yet, so say so
			// rather than silently leaving the assignee in place.
			return mcp.NewToolResultError(
				"Clearing an assignee is not supported by jirrabit's API yet. Assign the issue to someone else instead.",
			), nil
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		client, shaper, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp editJiraIssue] %s", args.IssueIDOrKey)

		var current jira.Issue
		if err := client.Get(ctx, fmt.Sprintf("issues/%s/", url.PathEscape(args.IssueIDOrKey)), &current); err != nil {
			return toolError(err)
		}

		// Only keys the caller actually set appear in the body. For a PATCH,
		// an absent key and an empty one mean different things, so this cannot
		// be a struct with zero values.
		payload := map[string]any{}
		if args.Summary != nil {
			payload["summary"] = *args.Summary
		}
		if args.Description != nil {
			payload["description"] = *args.Description
		}
		if args.StoryPoints != nil {
			payload["story_points"] = *args.StoryPoints
		}
		if args.DueDate != nil {
			payload["due_date"] = *args.DueDate
		}
		if args.Assignee != nil {
			id, err := resolveAssignee(ctx, client, current.Project, *args.Assignee)
			if err != nil {
				return toolError(err)
			}
			payload["assignee_id"] = id
		}
		// As on create: a named argument beats the same key inside `fields`.
		for key, value := range map[string]*int{
			"priority_id":      args.PriorityID,
			"status_id":        args.StatusID,
			"sprint_id":        args.SprintID,
			"epic_id":          args.EpicID,
			"estimate_minutes": args.EstimateMinutes,
		} {
			if value != nil {
				payload[key] = *value
			}
		}
		// "" is the way to say "no parent", so this is a pointer on the Go side
		// and an always-present string on the wire when the caller said anything
		// at all. Sending nothing must not detach: an agent that edits a summary
		// did not ask to orphan the issue.
		if args.Parent != nil {
			payload["parent"] = *args.Parent
		}
		// Always sent when non-nil, and nil when absent, so "no argument" and
		// "an empty list" stay different: the second clears the labels.
		if args.Labels != nil {
			payload["labels"] = args.Labels
		}
		for key, value := range extra {
			payload[key] = value
		}

		var updated jira.Issue
		path := fmt.Sprintf("issues/%s/", url.PathEscape(args.IssueIDOrKey))
		if err := client.Patch(ctx, path, payload, &updated); err != nil {
			return toolError(err)
		}
		return jsonResult(shaper.Issue(updated))
	}
}

// transitionJiraIssue is editJiraIssue's status half under the name Atlassian
// uses, so an agent that has driven real Jira reaches for the right tool
// without reading this server's source. The write itself is the same PATCH
// editJiraIssue issues, which is what applies the workflow check on the server.
func transitionJiraIssue(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.TransitionIssueArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		if args.StatusID == 0 && args.StatusName == "" {
			return mcp.NewToolResultError(
				"Pass statusId (from listJiraStatuses) or statusName, e.g. \"In Progress\"",
			), nil
		}
		client, shaper, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		log.Printf("[jirrabit-mcp transitionJiraIssue] %s", args.IssueIDOrKey)

		statusID := args.StatusID
		if statusID == 0 {
			statusID, err = lookupStatusID(ctx, client, args.StatusName)
			if err != nil {
				return toolError(err)
			}
		}

		var updated jira.Issue
		path := fmt.Sprintf("issues/%s/", url.PathEscape(args.IssueIDOrKey))
		if err := client.Patch(ctx, path, map[string]any{"status_id": statusID}, &updated); err != nil {
			return toolError(err)
		}
		return jsonResult(shaper.Issue(updated))
	}
}

// lookupStatusID resolves a status name to its numeric id. The API's
// /statuses/ list is the only place both halves live, so a name has to be
// looked up before the PATCH can name it.
func lookupStatusID(ctx context.Context, client *jira.Client, name string) (int, error) {
	var envelope struct {
		Items []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
		Values []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"values"`
	}
	if err := client.Get(ctx, "statuses/", &envelope); err != nil {
		return 0, err
	}
	rows := envelope.Items
	if len(rows) == 0 {
		rows = envelope.Values
	}
	wanted := strings.TrimSpace(name)
	for _, row := range rows {
		if strings.EqualFold(row.Name, wanted) {
			return row.ID, nil
		}
	}
	available := make([]string, 0, len(rows))
	for _, row := range rows {
		available = append(available, row.Name)
	}
	return 0, fmt.Errorf("no status named %q. This instance has: %s",
		wanted, strings.Join(available, ", "))
}

func addOrEditJiraIssueComment(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.AddOrEditCommentArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		client, shaper, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		if args.Body == "" {
			return mcp.NewToolResultError("body is required"), nil
		}
		issuePath := fmt.Sprintf("issues/%s/", url.PathEscape(args.IssueIDOrKey))

		if args.CommentID != "" {
			// Was refused here for a release, while jirrabit had no PATCH for
			// comments. It does now, so this is a real edit and not a refusal.
			log.Printf("[jirrabit-mcp addOrEditJiraIssueComment] edit %s on %s",
				args.CommentID, args.IssueIDOrKey)
			var comment jira.Comment
			path := fmt.Sprintf("%scomments/%s/", issuePath, url.PathEscape(args.CommentID))
			if err := client.Patch(ctx, path, map[string]string{"body": args.Body}, &comment); err != nil {
				return toolError(err)
			}
			return jsonResult(shaper.Comment(comment))
		}

		// Not a pending API change: jirrabit has no groups or roles at all, so
		// a restricted comment has nothing to be restricted to. Rejected rather
		// than dropped, because an agent sending visibilityType is usually
		// carrying it over from Atlassian's tool and would otherwise get a
		// public comment back with no indication that it was public.
		if args.VisibilityType != "" || args.VisibilityValue != "" {
			return mcp.NewToolResultError(
				"Comment visibility is not available here, so nothing was posted. jirrabit has no " +
					"groups or roles, so there is nothing for visibilityType to restrict a comment to; " +
					"its only equivalent is an internal-only comment, which jirrabit's API does not expose. " +
					"To add an ordinary comment, drop visibilityType and visibilityValue.",
			), nil
		}

		log.Printf("[jirrabit-mcp addOrEditJiraIssueComment] create on %s", args.IssueIDOrKey)
		var comment jira.Comment
		if err := client.Post(ctx, issuePath+"comments/", map[string]string{"body": args.Body}, &comment); err != nil {
			return toolError(err)
		}
		return jsonResult(shaper.Comment(comment))
	}
}

func addOrEditJiraIssueWorklog(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.AddOrEditWorkLogArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		client, shaper, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}

		if args.WorklogID != "" {
			// jirrabit has no PATCH for a worklog, and it is not worth faking:
			// the row also feeds the issue's time totals, so an edit has to move
			// those too, and only the create path does. deleteJiraIssueWorklog
			// and a fresh addJiraIssue-with-time will get there.
			return mcp.NewToolResultErrorf(
				"Editing an existing worklog is not supported by jirrabit's API yet (it exposes no PATCH "+
					"/api/v1/issues/%s/worklogs/{id}/, and the edit would have to adjust the issue's time "+
					"totals as well). To correct an entry, deleteJiraIssueWorklog removes it and this tool "+
					"logs the replacement; to log time, omit worklogId.",
				args.IssueIDOrKey,
			), nil
		}

		// These four are declared because Atlassian's tool declares them, and
		// they used to be accepted and thrown away: the payload below is
		// minutes and comment, nothing else. An agent that said "log 2h against
		// Tuesday, and drop the remaining estimate by 2h" got a success, an
		// entry timestamped now, and an untouched estimate.
		//
		// jirrabit's WorkLogIn carries only minutes and comment, and its
		// IssuePatch has no estimate field, so honouring them is an API change
		// rather than a bug here. Naming the field that would have to exist is
		// what turns "that did not work" into a decision the caller can make.
		if dropped := droppedWorklogArgs(args); dropped != "" {
			return mcp.NewToolResultErrorf(
				"%s cannot be honoured by jirrabit's API yet, so nothing was logged. "+
					"POST /api/v1/issues/%s/worklogs/ accepts only minutes and comment, "+
					"and changing the remaining estimate needs estimate_minutes on IssuePatch, "+
					"which is read-only today. To log the time now, drop %s.",
				dropped, args.IssueIDOrKey, dropped,
			), nil
		}

		minutes, err := worklogMinutes(args)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		log.Printf("[jirrabit-mcp addOrEditJiraIssueWorklog] %s %dm", args.IssueIDOrKey, minutes)

		var worklog jira.WorkLog
		path := fmt.Sprintf("issues/%s/worklogs/", url.PathEscape(args.IssueIDOrKey))
		payload := map[string]any{"minutes": minutes}
		if args.Comment != "" {
			payload["comment"] = args.Comment
		}
		if err := client.Post(ctx, path, payload, &worklog); err != nil {
			return toolError(err)
		}
		return jsonResult(shaper.WorkLog(worklog))
	}
}

// worklogMinutes normalises the two ways a duration can arrive. jirrabit takes
// whole minutes; Jira sends either seconds or a "2h 30m" string.
// droppedWorklogArgs names the worklog arguments this server cannot honour, or
// "" when the call is one jirrabit can actually store.
//
// Listed together and named in the message, because an agent that sent one of
// them needs to know it was not stored: the alternative, accepting the call and
// logging the time anyway, is how "reduce the estimate by 2h" came to mean
// "logged 2h, estimate unchanged, no error".
func droppedWorklogArgs(args schema.AddOrEditWorkLogArgs) string {
	var dropped []string
	if args.Started != "" {
		dropped = append(dropped, "started")
	}
	if args.NewEstimate != "" {
		dropped = append(dropped, "newEstimate")
	}
	if args.AdjustEstimate != "" {
		dropped = append(dropped, "adjustEstimate")
	}
	if args.ReduceBy != "" {
		dropped = append(dropped, "reduceBy")
	}
	return strings.Join(dropped, " and ")
}

func worklogMinutes(args schema.AddOrEditWorkLogArgs) (int, error) {
	if args.TimeSpentSeconds != nil {
		if *args.TimeSpentSeconds <= 0 {
			return 0, fmt.Errorf("timeSpentSeconds must be greater than 0")
		}
		// Round up: logging 90 seconds is 2 minutes of jirrabit's time, and
		// truncating would silently lose the remainder.
		seconds := *args.TimeSpentSeconds
		return (seconds + 59) / 60, nil
	}
	if args.TimeSpent != "" {
		minutes, err := jira.ParseJiraDuration(args.TimeSpent)
		if err != nil {
			return 0, err
		}
		return minutes, nil
	}
	return 0, fmt.Errorf("provide timeSpentSeconds (or timeSpent in '2h 30m' form) to log time")
}

// --- reversible removal ---------------------------------------------------
//
// Deliberately not in the two-step group. Archiving a comment and archiving an
// issue both keep the data and both have an inverse, so requiring a
// confirmation token for them would be ceremony around an undo button. What
// they do instead is say how to get the thing back, because a caller who
// archived something under the impression it was deleted deserves to know
// otherwise.

func archiveJiraIssue(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ArchiveIssueArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if args.IssueIDOrKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		client, shaper, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		archived := true
		if args.Archived != nil {
			archived = *args.Archived
		}
		verb := "archive"
		if !archived {
			verb = "unarchive"
		}
		log.Printf("[jirrabit-mcp archiveJiraIssue] %s %s on %s",
			verb, args.IssueIDOrKey, client.BaseURL())

		var issue jira.Issue
		path := fmt.Sprintf("issues/%s/", url.PathEscape(args.IssueIDOrKey))
		if err := client.Patch(ctx, path, map[string]any{"archived": archived}, &issue); err != nil {
			return toolError(err)
		}
		shaped := shaper.Issue(issue)
		result := map[string]any{
			"issueIdOrKey": args.IssueIDOrKey,
			"archived":     shaped.Fields.Archived,
		}
		if archived {
			// The half of the answer people forget: nothing was lost, and this
			// is the call that brings it back.
			result["reversible"] = true
			result["undo"] = "archiveJiraIssue with archived: false"
		}
		return jsonResult(result)
	}
}

// createProvidedNames and editProvidedNames list only what the caller actually
// passed, which is what normaliseFields needs to tell a real duplicate from an
// argument that merely exists. A pointer is nil or it is not; a string is empty
// or it is not. issue_type_id is special because two arguments can produce it and
// either counts.
func createProvidedNames(args schema.CreateIssueArgs) map[string]bool {
	provided := map[string]bool{
		"summary":       true,
		"assignee_id":   args.Assignee != "",
		"due_date":      args.DueDate != "",
		"parent":        args.Parent != "",
		"issue_type_id": args.IssueTypeID != nil || args.IssueTypeName != "",
	}
	if args.Description != "" {
		provided["description"] = true
	}
	for key, value := range map[string]*int{
		"story_points":     args.StoryPoints,
		"priority_id":      args.PriorityID,
		"status_id":        args.StatusID,
		"sprint_id":        args.SprintID,
		"epic_id":          args.EpicID,
		"estimate_minutes": args.EstimateMinutes,
	} {
		if value != nil {
			provided[key] = true
		}
	}
	return provided
}

func editProvidedNames(args schema.EditIssueArgs) map[string]bool {
	provided := map[string]bool{
		"parent":      args.Parent != nil,
		"assignee_id": args.Assignee != nil,
		"due_date":    args.DueDate != nil,
		"description": args.Description != nil,
		"labels":      args.Labels != nil,
	}
	if args.Summary != nil {
		provided["summary"] = true
	}
	for key, value := range map[string]*int{
		"story_points":     args.StoryPoints,
		"status_id":        args.StatusID,
		"priority_id":      args.PriorityID,
		"sprint_id":        args.SprintID,
		"epic_id":          args.EpicID,
		"estimate_minutes": args.EstimateMinutes,
	} {
		if value != nil {
			provided[key] = true
		}
	}
	return provided
}
