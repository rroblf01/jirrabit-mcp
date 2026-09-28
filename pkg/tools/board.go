package tools

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/rroblf01/jirrabit-mcp/pkg/jira"
	"github.com/rroblf01/jirrabit-mcp/pkg/schema"
)

// registerBoardTools wires the writes the kanban performs.
//
// The gap this fills is not "an agent could not read the board" — searchJira
// with a JQL clause lists issues with their statuses. It is that an agent could
// not *place* anything. PATCH sets a status, which is most of the move, but
// `rank` — the card's index inside its (project, status) column — had no endpoint
// at all, so every issue an agent touched landed at the end of a column and the
// board slowly became alphabetical-by-accident.
//
// These endpoints exist rather than being left to the client because rank is an
// invariant, not a field: a column is a dense 0..n-1 run, and setting it by hand
// from five places is how a board grows a hole. So they route through the same
// board.views helpers a drag uses, which renumber the whole column.

func registerBoardTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("moveJiraIssue",
		mcp.WithDescription(
			"Move a work item to a status and a position on the board. Use it to change a card's "+
				"column, to reorder cards inside one, or both at once.\n\n"+
				"Omit position to put the card at the end of its column, which is the only honest "+
				"thing to do when no position is given. Omit targetStatus to keep the current "+
				"status and only change position.\n\n"+
				"A transition the workflow forbids is refused rather than forced, and so is a "+
				"transition to a status name that does not exist. Use listJiraTransitions first if "+
				"you are unsure."),
		mcp.WithTitleAnnotation("Move issue on the board"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.MoveIssueArgs](),
	), moveJiraIssue(d))

	s.AddTool(mcp.NewTool("reorderJiraBoardColumn",
		mcp.WithDescription(
			"Renumber one board column to a given order.\n\n"+
				"Pass the whole column, as the board's own drag does: two cards swapping places is "+
				"then the same call as a column of twenty, and there is no index arithmetic for the "+
				"caller to get wrong.\n\n"+
				"A shorter list is legal and means 'these to the top, the rest keep their order', "+
				"because that is what a stale view of the board sends and refusing it would make the "+
				"board unusable as soon as somebody else moved a card. A key that is not in this "+
				"project is an error.\n\n"+
				"Returns the resulting order and the new index of each card, so a card that moved "+
				"between columns is visible in the answer and not only in the board's rendering."),
		mcp.WithTitleAnnotation("Reorder a board column"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.ReorderBoardColumnArgs](),
	), reorderJiraBoardColumn(d))

	s.AddTool(mcp.NewTool("bulkUpdateJiraBoard",
		mcp.WithDescription(
			"Apply one field change to several work items at once — a status, an assignee, a "+
				"priority, a sprint, an epic, or adding or removing one label.\n\n"+
				"Prefer this to a loop of editJiraIssue: it is one call, and it reports which "+
				"issues it could not change instead of failing on the first one. A status change "+
				"the workflow forbids is skipped, not forced, and the issue key is returned in "+
				"`skipped` with a note saying why.\n\n"+
				"Deleting is deliberately not an action here. Use deleteJiraIssue, which shows what "+
				"is about to disappear and asks for an explicit confirmation."),
		mcp.WithTitleAnnotation("Bulk update work items"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithInputSchema[schema.BulkUpdateBoardArgs](),
	), bulkUpdateJiraBoard(d))
}

func moveJiraIssue(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.MoveIssueArgs
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
		if args.TargetStatus == "" && args.Position == nil {
			return mcp.NewToolResultError(
				"nothing to do: give targetStatus, position, or both"), nil
		}

		// The endpoint takes ids because a status name is not unique enough to be
		// an identifier, and resolving it here means the tool can take the name an
		// agent actually has.
		body := map[string]any{}
		if args.TargetStatus != "" {
			id, err := statusID(ctx, client, args.TargetStatus)
			if err != nil {
				return toolError(err)
			}
			body["status_id"] = id
		}
		if args.Position != nil {
			if *args.Position < 0 {
				return mcp.NewToolResultError("position is 0-based and cannot be negative"), nil
			}
			body["rank"] = *args.Position
		}
		log.Printf("[jirrabit-mcp moveJiraIssue] %s %v", args.IssueIDOrKey, body)

		var moved jira.Issue
		path := fmt.Sprintf("issues/%s/move/", url.PathEscape(args.IssueIDOrKey))
		if err := client.Post(ctx, path, body, &moved); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{
			"issueIdOrKey": moved.Key,
			"issue":        shaper.Issue(moved),
		})
	}
}

func reorderJiraBoardColumn(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.ReorderBoardColumnArgs
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
		statusID, err := statusID(ctx, client, args.TargetStatus)
		if err != nil {
			return toolError(err)
		}
		if len(args.IssueKeys) == 0 {
			return mcp.NewToolResultError("issueKeys is required and cannot be empty"), nil
		}
		// A duplicate is silently de-duplicated server-side, so sending one twice
		// would look like it worked while meaning something else.
		if dupes := repeated(args.IssueKeys); len(dupes) > 0 {
			return mcp.NewToolResultError(fmt.Sprintf(
				"issueKeys repeats %s; a card can only appear once in a column",
				strings.Join(dupes, ", "))), nil
		}
		log.Printf("[jirrabit-mcp reorderJiraBoardColumn] %s status=%d keys=%d",
			key, statusID, len(args.IssueKeys))

		var result struct {
			StatusID int      `json:"statusId"`
			Keys     []string `json:"keys"`
			Moved    int      `json:"moved"`
		}
		path := fmt.Sprintf("projects/%s/board/reorder/", url.PathEscape(key))
		if err := client.Post(ctx, path, map[string]any{
			"status_id": statusID,
			"keys":      args.IssueKeys,
		}, &result); err != nil {
			return toolError(err)
		}
		// The index of each card, so the caller can tell what actually moved rather
		// than inferring it from the order it sent.
		indexes := make([]map[string]any, 0, len(result.Keys))
		for i, key := range result.Keys {
			indexes = append(indexes, map[string]any{"issueIdOrKey": key, "position": i})
		}
		return jsonResult(map[string]any{
			"projectKeyOrId": key,
			"statusId":       result.StatusID,
			"order":          result.Keys,
			"cards":          indexes,
			"renamed":        result.Moved,
			"partial":        result.Moved < len(args.IssueKeys),
		})
	}
}

func bulkUpdateJiraBoard(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args schema.BulkUpdateBoardArgs
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
		if len(args.IssueKeys) == 0 {
			return mcp.NewToolResultError("issueKeys is required and cannot be empty"), nil
		}
		if args.Action == "delete" {
			// Named explicitly, because it is the one action a caller reaches for
			// and the endpoint refuses it. Silently not supporting it would read
			// as "nothing happened".
			return mcp.NewToolResultError(
				"deleting several issues is not available here: call deleteJiraIssue for each " +
					"one, so each shows what it will remove and asks for confirmation"), nil
		}

		// The value is resolved to the id the endpoint wants, for the same reason
		// status is: a name is not an identifier.
		value := args.Value
		switch args.Action {
		case "status":
			if value == "" {
				return mcp.NewToolResultError("action 'status' needs a status name in value"), nil
			}
			id, err := statusID(ctx, client, value)
			if err != nil {
				return toolError(err)
			}
			value = fmt.Sprint(id)
		case "priority":
			if value == "" {
				return mcp.NewToolResultError("action 'priority' needs a priority name in value"), nil
			}
			id, err := lookupID(ctx, client, "priorities/", "prioridad", value)
			if err != nil {
				return toolError(err)
			}
			value = fmt.Sprint(id)
		case "epic":
			if value == "" {
				return mcp.NewToolResultError("action 'epic' needs an epic id in value"), nil
			}
		}
		log.Printf("[jirrabit-mcp bulkUpdateJiraBoard] %s action=%s over %d",
			key, args.Action, len(args.IssueKeys))

		var result struct {
			Changed []string       `json:"changed"`
			Skipped []string       `json:"skipped"`
			Notes   map[string]any `json:"notes"`
		}
		path := fmt.Sprintf("projects/%s/board/bulk-update/", url.PathEscape(key))
		if err := client.Post(ctx, path, map[string]any{
			"keys":   args.IssueKeys,
			"action": args.Action,
			"value":  value,
		}, &result); err != nil {
			return toolError(err)
		}
		if result.Skipped == nil {
			result.Skipped = []string{}
		}
		return jsonResult(map[string]any{
			"projectKeyOrId": key,
			"action":         args.Action,
			"changed":        result.Changed,
			"skipped":        result.Skipped,
			"notes":          result.Notes,
			"allSucceeded":   len(result.Skipped) == 0,
		})
	}
}

// statusID resolves a status name to its id. A name is what an agent has, and
// the write endpoints take ids because a name is not an identifier.
func statusID(ctx context.Context, client *jira.Client, name string) (int, error) {
	return namedID(ctx, client, "statuses/", "estado", name)
}

// lookupID resolves a name in any of the metadata vocabularies. A separate
// function from statusID so each call site names the thing it is looking for, and
// the error says which list was searched.
func lookupID(ctx context.Context, client *jira.Client, path, noun, name string) (int, error) {
	return namedID(ctx, client, path, noun, name)
}

// namedID finds one entry of a metadata endpoint by name, case-insensitively.
//
// The available names go in the error, because a caller that guessed "Done" when
// the workflow calls it "Closed" would otherwise have to spend a call to find out
// what the right answer was.
func namedID(ctx context.Context, client *jira.Client, path, noun, name string) (int, error) {
	items, _, err := jira.List[statusDTO](ctx, client, jira.WithPage(path, 1, 200))
	if err != nil {
		return 0, err
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(name)) {
			return item.ID, nil
		}
		names = append(names, item.Name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return 0, fmt.Errorf("no hay ningún %s en esta instancia", noun)
	}
	return 0, fmt.Errorf("no hay un %s llamado %q; los que existen son: %s",
		noun, name, strings.Join(names, ", "))
}

// repeated returns the values that appear more than once, sorted, for an error
// message. A set is not enough: the message has to name them.
func repeated(values []string) []string {
	counts := map[string]int{}
	for _, value := range values {
		counts[value]++
	}
	var dupes []string
	for value, n := range counts {
		if n > 1 {
			dupes = append(dupes, value)
		}
	}
	sort.Strings(dupes)
	return dupes
}

// countJQL runs a JQL query and returns only the total.
//
// Used by the delete previews to say how many rows are about to change without
// pulling them. The count comes from the page envelope, so a query matching
// thousands of issues costs the same as one matching three.
func countJQL(ctx context.Context, client *jira.Client, jql string) (int, error) {
	var envelope struct {
		Total int `json:"total"`
	}
	if err := client.Get(ctx, "search?jql="+url.QueryEscape(jql)+"&size=1", &envelope); err != nil {
		return 0, err
	}
	return envelope.Total, nil
}
