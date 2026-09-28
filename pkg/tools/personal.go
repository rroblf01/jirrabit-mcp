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

// registerPersonalTools wires the state that belongs to a person rather than to
// an issue: what they pinned, what they are timing, what they have muted, and
// the scaffolding they use to open work.
//
// None of this had an HTTP path, so an agent could read an issue and change an
// issue and still had no way to answer "what am I working on right now" — the
// question a board is mostly asked.
//
// The reaction set is fixed rather than open. jirrabit validates it on write, so
// an agent picking its own emoji would get a 400 and have to guess again; the
// list is in the tool description so the guess is not needed.

var reactionEmoji = []string{"tada", "rocket", "eyes", "heart", "fire", "thinking"}

func registerPersonalTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("listJiraPins",
		mcp.WithDescription(
			"List what the caller has pinned, newest first. Pins are per-user, so this "+
				"returns that user's pins and nobody else's."),
		mcp.WithTitleAnnotation("List pinned items"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.ListPinsArgs](),
	), listJiraPins(d))

	s.AddTool(mcp.NewTool("pinJiraItem",
		mcp.WithDescription(
			"Pin an issue or a project so it stays at the top of the caller's board. Give "+
				"exactly one of issueIdOrKey or projectKeyOrId. Pinning twice is not an error."),
		mcp.WithTitleAnnotation("Pin an item"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.PinIssueArgs](),
	), pinJiraItem(d))

	s.AddTool(mcp.NewTool("unpinJiraItem",
		mcp.WithDescription(
			"Remove one of the caller's pins, by the id from listJiraPins. Somebody else's "+
				"pin is not reachable this way."),
		mcp.WithTitleAnnotation("Unpin an item"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.UnpinItemArgs](),
	), unpinJiraItem(d))

	s.AddTool(mcp.NewTool("getJiraIssueTimer",
		mcp.WithDescription(
			"The caller's running timer on an issue, and how long it has been running. "+
				"Errors when no timer is running on it."),
		mcp.WithTitleAnnotation("Get issue timer"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.GetIssueTimerArgs](),
	), getJiraIssueTimer(d))

	s.AddTool(mcp.NewTool("startJiraIssueTimer",
		mcp.WithDescription(
			"Start timing work on an issue. One timer per person at a time: starting while "+
				"another is running is refused rather than moving it, because moving it would "+
				"throw away the minutes so far. Stop that one first."),
		mcp.WithTitleAnnotation("Start issue timer"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.StartIssueTimerArgs](),
	), startJiraIssueTimer(d))

	s.AddTool(mcp.NewTool("stopJiraIssueTimer",
		mcp.WithDescription(
			"Stop the caller's timer on an issue and record the elapsed minutes as a work "+
				"log. Returns the work log, with its id and the minutes it credited.\n\n"+
				"This is the step that makes time visible to the rest of the team, so prefer it "+
				"over leaving a timer running and reading the total later."),
		mcp.WithTitleAnnotation("Stop issue timer"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.StopIssueTimerArgs](),
	), stopJiraIssueTimer(d))

	s.AddTool(mcp.NewTool("snoozeJiraIssueNotifications",
		mcp.WithDescription(
			"Mute notifications for one issue until a given instant. The instant must be in "+
				"the future. Snoozing again replaces the deadline rather than adding to it."),
		mcp.WithTitleAnnotation("Snooze issue notifications"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.SnoozeIssueArgs](),
	), snoozeJiraIssue(d))

	s.AddTool(mcp.NewTool("unsnoozeJiraIssueNotifications",
		mcp.WithDescription("Unmute an issue that was snoozed, immediately."),
		mcp.WithTitleAnnotation("Unsnooze issue notifications"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.UnsnoozeIssueArgs](),
	), unsnoozeJiraIssue(d))

	s.AddTool(mcp.NewTool("listJiraCommentReactions",
		mcp.WithDescription(
			"The reactions on a comment, as counts per emoji, plus which of them are the "+
				"caller's. Counts rather than a list of people: a reaction is a signal."),
		mcp.WithTitleAnnotation("List comment reactions"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.ListCommentReactionsArgs](),
	), listJiraCommentReactions(d))

	s.AddTool(mcp.NewTool("reactToJiraComment",
		mcp.WithDescription(
			"React to a comment with one of: "+strings.Join(reactionEmoji, ", ")+".\n\n"+
				"Reacting with the same emoji twice is not a second reaction — it is the same "+
				"one. jirrabit validates the emoji, so anything outside that list is refused."),
		mcp.WithTitleAnnotation("React to a comment"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.ReactToCommentArgs](),
	), reactToJiraComment(d))

	s.AddTool(mcp.NewTool("removeJiraCommentReaction",
		mcp.WithDescription("Take back the caller's own reaction to a comment."),
		mcp.WithTitleAnnotation("Remove a comment reaction"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.RemoveCommentReactionArgs](),
	), removeJiraCommentReaction(d))

	s.AddTool(mcp.NewTool("listJiraIssueBranchLinks",
		mcp.WithDescription(
			"The branches and commits linked to an issue, newest first. Use it to see what "+
				"code has already been written for a work item."),
		mcp.WithTitleAnnotation("List issue branch links"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.ListBranchLinksArgs](),
	), listJiraBranchLinks(d))

	s.AddTool(mcp.NewTool("linkJiraBranchToIssue",
		mcp.WithDescription(
			"Link a branch or a commit to an issue, so the code and the work item point at "+
				"each other. Idempotent on the issue, branch and commit together, so linking "+
				"the same branch twice does not create a second row."),
		mcp.WithTitleAnnotation("Link a branch to an issue"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.LinkBranchToIssueArgs](),
	), linkJiraBranchToIssue(d))

	s.AddTool(mcp.NewTool("unlinkJiraBranchFromIssue",
		mcp.WithDescription(
			"Remove one branch link from an issue, by the id from listJiraIssueBranchLinks. "+
				"The branch itself is not touched: this only drops the link."),
		mcp.WithTitleAnnotation("Unlink a branch from an issue"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.UnlinkBranchFromIssueArgs](),
	), unlinkJiraBranchFromIssue(d))

	s.AddTool(mcp.NewTool("listJiraIssueTemplates",
		mcp.WithDescription(
			"The reusable scaffolds for opening work in a project: the fields and labels a "+
				"given kind of issue should start with."),
		mcp.WithTitleAnnotation("List issue templates"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.ListIssueTemplatesArgs](),
	), listJiraIssueTemplates(d))

	s.AddTool(mcp.NewTool("createJiraIssueTemplate",
		mcp.WithDescription(
			"Create an issue template in a project. Needs admin on the project: it is project "+
				"configuration, and a template is what every future issue of that kind starts as.\n\n"+
				"The name must be unique within the project."),
		mcp.WithTitleAnnotation("Create issue template"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.CreateIssueTemplateArgs](),
	), createJiraIssueTemplate(d))

}

// --- pins -------------------------------------------------------------------

type pinDTO struct {
	ID        int    `json:"id"`
	Issue     string `json:"issue"`
	Project   string `json:"project"`
	CreatedAt string `json:"created_at"`
}

func listJiraPins(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListPinsArgs
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
		log.Printf("[jirrabit-mcp listJiraPins] %s page=%d size=%d", client.BaseURL(), page, size)

		items, meta, err := jira.List[pinDTO](ctx, client, jira.WithPage("pins/", page, size))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count:         meta.Count,
			StartAt:       page,
			MaxResults:    size,
			NextPageToken: jira.NextToken(meta),
			Values:        items,
		})
	}
}

func pinJiraItem(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.PinIssueArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if (args.IssueIDOrKey == "") == (args.ProjectKeyOrID == "") {
			return mcp.NewToolResultError(
				"give exactly one of issueIdOrKey or projectKeyOrId: a pin is of one thing or " +
					"the other"), nil
		}
		body := map[string]any{}
		if args.IssueIDOrKey != "" {
			body["issue"] = args.IssueIDOrKey
		} else {
			key, err := projectKey(ctx, client, args.ProjectKeyOrID)
			if err != nil {
				return toolError(err)
			}
			body["project"] = key
		}
		var pin pinDTO
		if err := client.Post(ctx, "pins/", body, &pin); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"pin": pin})
	}
}

func unpinJiraItem(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.UnpinItemArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.PinID <= 0 {
			return mcp.NewToolResultError("pinId is required, from listJiraPins"), nil
		}
		var out map[string]any
		path := fmt.Sprintf("pins/%d/", args.PinID)
		if err := client.Delete(ctx, path, &out); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"unpinned": args.PinID})
	}
}

// --- timers -----------------------------------------------------------------

type timerDTO struct {
	Issue     string `json:"issue"`
	StartedAt string `json:"started_at"`
	Running   bool   `json:"running"`
}

func getJiraIssueTimer(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return issueTimerGet(d)
}

func startJiraIssueTimer(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.StartIssueTimerArgs
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
		log.Printf("[jirrabit-mcp startJiraIssueTimer] %s", args.IssueIDOrKey)
		var timer timerDTO
		path := fmt.Sprintf("issues/%s/timer/start/", url.PathEscape(args.IssueIDOrKey))
		if err := client.Post(ctx, path, map[string]any{}, &timer); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"timer": timer})
	}
}

func issueTimerGet(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.GetIssueTimerArgs
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
		var timer timerDTO
		path := fmt.Sprintf("issues/%s/timer/", url.PathEscape(args.IssueIDOrKey))
		if err := client.Get(ctx, path, &timer); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"timer": timer})
	}
}

type workLogDTO struct {
	ID          int    `json:"id"`
	Issue       string `json:"issue"`
	Author      string `json:"author"`
	Minutes     int    `json:"minutes"`
	Comment     string `json:"comment"`
	StartedAt   string `json:"started_at"`
	TimeSpentMn int    `json:"time_spent_minutes"`
}

func stopJiraIssueTimer(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.StopIssueTimerArgs
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
		log.Printf("[jirrabit-mcp stopJiraIssueTimer] %s", args.IssueIDOrKey)
		var worklog workLogDTO
		path := fmt.Sprintf("issues/%s/timer/stop/", url.PathEscape(args.IssueIDOrKey))
		if err := client.Post(ctx, path, map[string]any{}, &worklog); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{
			"worklog": worklog,
			"minutes": worklog.Minutes,
		})
	}
}

// --- snooze -----------------------------------------------------------------

func snoozeJiraIssue(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.SnoozeIssueArgs
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
		if args.Until == "" {
			return mcp.NewToolResultError("until is required, as an ISO-8601 instant in the future"), nil
		}
		log.Printf("[jirrabit-mcp snoozeJiraIssueNotifications] %s until=%s",
			args.IssueIDOrKey, args.Until)
		var out map[string]any
		path := fmt.Sprintf("issues/%s/snooze/", url.PathEscape(args.IssueIDOrKey))
		if err := client.Post(ctx, path, map[string]any{"until": args.Until}, &out); err != nil {
			return toolError(err)
		}
		return jsonResult(out)
	}
}

func unsnoozeJiraIssue(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.UnsnoozeIssueArgs
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
		var out map[string]any
		path := fmt.Sprintf("issues/%s/snooze/", url.PathEscape(args.IssueIDOrKey))
		if err := client.Delete(ctx, path, &out); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"unsnoozed": args.IssueIDOrKey})
	}
}

// --- reactions --------------------------------------------------------------

type reactionDTO struct {
	Comment int            `json:"comment"`
	Counts  map[string]int `json:"counts"`
	Mine    []string       `json:"mine"`
}

func listJiraCommentReactions(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListCommentReactionsArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.IssueIDOrKey == "" || args.CommentID <= 0 {
			return mcp.NewToolResultError(
				"issueIdOrKey and commentId are both required, the latter from the issue's comments"), nil
		}
		var reaction reactionDTO
		path := reactionPath(args.IssueIDOrKey, args.CommentID)
		if err := client.Get(ctx, path, &reaction); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"reactions": reaction})
	}
}

func reactToJiraComment(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ReactToCommentArgs
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
		// Checked here so the message lists the alternatives. jirrabit would refuse
		// it too, but with a bare 400 and no way to recover without a second call.
		if !supportedEmoji(args.Emoji) {
			return mcp.NewToolResultError(fmt.Sprintf(
				"%q is not a supported reaction; use one of: %s",
				args.Emoji, strings.Join(reactionEmoji, ", "))), nil
		}
		log.Printf("[jirrabit-mcp reactToJiraComment] %s comment=%d %s",
			args.IssueIDOrKey, args.CommentID, args.Emoji)
		var reaction reactionDTO
		if err := client.Post(ctx, reactionPath(args.IssueIDOrKey, args.CommentID),
			map[string]any{"emoji": args.Emoji}, &reaction); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"reactions": reaction})
	}
}

func removeJiraCommentReaction(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.RemoveCommentReactionArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.IssueIDOrKey == "" || args.CommentID <= 0 || args.Emoji == "" {
			return mcp.NewToolResultError("issueIdOrKey, commentId and emoji are all required"), nil
		}
		path := reactionPath(args.IssueIDOrKey, args.CommentID) +
			url.PathEscape(args.Emoji) + "/"
		var reaction reactionDTO
		if err := client.Delete(ctx, path, &reaction); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"reactions": reaction})
	}
}

func reactionPath(issueKey string, commentID int) string {
	return fmt.Sprintf("issues/%s/comments/%d/reactions/", url.PathEscape(issueKey), commentID)
}

func supportedEmoji(emoji string) bool {
	for _, candidate := range reactionEmoji {
		if candidate == emoji {
			return true
		}
	}
	return false
}

// --- branch links -----------------------------------------------------------

type branchLinkDTO struct {
	ID        int    `json:"id"`
	Issue     string `json:"issue"`
	Branch    string `json:"branch"`
	RepoURL   string `json:"repo_url"`
	CommitSHA string `json:"commit_sha"`
	Message   string `json:"message"`
	CreatedBy string `json:"created_by"`
	CreatedAt string `json:"created_at"`
}

func listJiraBranchLinks(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListBranchLinksArgs
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
		path := fmt.Sprintf("issues/%s/branches/", url.PathEscape(args.IssueIDOrKey))
		items, meta, err := jira.List[branchLinkDTO](ctx, client, jira.WithPage(path, page, size))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count:         meta.Count,
			StartAt:       page,
			MaxResults:    size,
			NextPageToken: jira.NextToken(meta),
			Values:        items,
		})
	}
}

func linkJiraBranchToIssue(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.LinkBranchToIssueArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.IssueIDOrKey == "" || strings.TrimSpace(args.Branch) == "" {
			return mcp.NewToolResultError("issueIdOrKey and branch are both required"), nil
		}
		log.Printf("[jirrabit-mcp linkJiraBranchToIssue] %s %s", args.IssueIDOrKey, args.Branch)
		var link branchLinkDTO
		path := fmt.Sprintf("issues/%s/branches/", url.PathEscape(args.IssueIDOrKey))
		if err := client.Post(ctx, path, map[string]any{
			"branch":     strings.TrimSpace(args.Branch),
			"repo_url":   args.RepoURL,
			"commit_sha": args.CommitSHA,
			"message":    args.Message,
		}, &link); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"branchLink": link})
	}
}

func unlinkJiraBranchFromIssue(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.UnlinkBranchFromIssueArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, args)
		if err != nil {
			return toolError(err)
		}
		if args.IssueIDOrKey == "" || args.LinkID <= 0 {
			return mcp.NewToolResultError(
				"issueIdOrKey and linkId are both required, the latter from listJiraIssueBranchLinks"), nil
		}
		var out map[string]any
		path := fmt.Sprintf("issues/%s/branches/%d/", url.PathEscape(args.IssueIDOrKey), args.LinkID)
		if err := client.Delete(ctx, path, &out); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"unlinked": args.LinkID})
	}
}

// --- issue templates --------------------------------------------------------

type issueTemplateDTO struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`
	IssueType string   `json:"issue_type"`
	Summary   string   `json:"summary"`
	Priority  string   `json:"priority"`
	Labels    []string `json:"labels"`
	CreatedBy string   `json:"created_by"`
	CreatedAt string   `json:"created_at"`
}

func listJiraIssueTemplates(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ListIssueTemplatesArgs
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
		path := fmt.Sprintf("projects/%s/issue-templates/", url.PathEscape(key))
		items, meta, err := jira.List[issueTemplateDTO](ctx, client, jira.WithPage(path, page, size))
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paged{
			Count:         meta.Count,
			StartAt:       page,
			MaxResults:    size,
			NextPageToken: jira.NextToken(meta),
			Values:        items,
		})
	}
}

func createJiraIssueTemplate(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.CreateIssueTemplateArgs
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
		if strings.TrimSpace(args.Name) == "" || args.IssueTypeID <= 0 {
			return mcp.NewToolResultError(
				"name and issueTypeId are required, the latter from listJiraIssueTypeMetadata"), nil
		}
		log.Printf("[jirrabit-mcp createJiraIssueTemplate] %s %s", key, args.Name)
		var template issueTemplateDTO
		path := fmt.Sprintf("projects/%s/issue-templates/", url.PathEscape(key))
		body := map[string]any{
			"name":          strings.TrimSpace(args.Name),
			"issue_type_id": args.IssueTypeID,
			"summary":       args.Summary,
			"description":   args.Description,
		}
		if args.PriorityID != nil {
			body["priority_id"] = *args.PriorityID
		}
		if len(args.Labels) > 0 {
			body["labels"] = args.Labels
		}
		if err := client.Post(ctx, path, body, &template); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{"issueTemplate": template})
	}
}

func deleteJiraIssueTemplate(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.DeleteIssueTemplateArgs
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
		if args.TemplateID <= 0 {
			return mcp.NewToolResultError("templateId is required, from listJiraIssueTemplates"), nil
		}
		path := fmt.Sprintf("projects/%s/issue-templates/%d/", url.PathEscape(key), args.TemplateID)
		var out map[string]any
		// DeleteOnce, not Delete: two calls are two separate decisions, and a
		// second one to change your mind should be refused.
		if err := client.DeleteOnce(ctx, path, &out); err != nil {
			return toolError(err)
		}
		return jsonResult(out)
	}
}

// registerPersonalDeleteTools wires the one personal write that cannot be undone.
//
// Behind the delete flag, like every other irreversible tool, and confirmed in two
// steps. Deleting a template only removes the scaffold: the issues created from it
// stay exactly as they are, which is why the preview says so rather than counting
// issues as if they were about to go.
func registerPersonalDeleteTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("deleteJiraIssueTemplate",
		mcp.WithDescription(
			"Delete one issue template. Irreversible, so the first call returns a preview and "+
				"a confirmation token and changes nothing; call it again with the token to "+
				"apply. Show the preview to the user and wait for their agreement before the "+
				"second call."),
		mcp.WithTitleAnnotation("Delete issue template"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.DeleteIssueTemplateArgs](),
	), deleteJiraIssueTemplate(d))
}
