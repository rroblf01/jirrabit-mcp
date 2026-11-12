package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/rroblf01/jirrabit-mcp/pkg/jira"
)

// jirrabit's write endpoints identify related records by numeric id, while
// agents think in names and usernames. These helpers bridge the two, and they
// are why jirrabit needs the small metadata endpoints they call
// (GET /api/v1/issue-types/ and GET /api/v1/users/search/).

// userDTO is the subset of jirrabit's user search results this adapter uses.
type userDTO struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	IsActive    *bool  `json:"is_active"`
}

// errAmbiguousMatch is returned when a name matches more than one record. Picking
// one would be a coin flip, so the ambiguity is reported instead.
var errAmbiguousMatch = errors.New("ambiguous match")

// resolveIssueType turns an issue type name or id into the id jirrabit wants.
//
// An explicit id always wins. A name is matched case-insensitively. When neither
// is given, nil is returned and jirrabit picks its default — which is why the
// create tool reports which type it ended up with.
func resolveIssueType(ctx context.Context, client *jira.Client, name string, id *int) (*int, error) {
	if id != nil {
		return id, nil
	}
	if name == "" {
		return nil, nil
	}
	types, err := fetchIssueTypes(ctx, client)
	if err != nil {
		return nil, err
	}
	var matches []issueTypeDTO
	for _, candidate := range types {
		if strings.EqualFold(candidate.Name, name) {
			matches = append(matches, candidate)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no issue type named %q. Available: %s", name, joinNames(types))
	case 1:
		found := matches[0].ID
		return &found, nil
	default:
		return nil, fmt.Errorf("%w: %q matches %d issue types", errAmbiguousMatch, name, len(matches))
	}
}

// fetchIssueTypes returns every configured issue type.
func fetchIssueTypes(ctx context.Context, client *jira.Client) ([]issueTypeDTO, error) {
	items, _, err := jira.List[issueTypeDTO](ctx, client, "issue-types/?size=200")
	if err != nil {
		return nil, err
	}
	return items, nil
}

// resolveAssignee turns a username into the user id jirrabit validates against
// project membership. An exact username match is required; a partial match is
// rejected rather than guessed, because assigning the wrong person is worse
// than failing.
func resolveAssignee(ctx context.Context, client *jira.Client, projectKey, username string) (int, error) {
	query := url.Values{}
	query.Set("query", username)
	items, _, err := jira.List[userDTO](ctx, client, "users/search/?"+query.Encode()+"&size=50")
	if err != nil {
		return 0, err
	}
	var exact []userDTO
	for _, candidate := range items {
		if strings.EqualFold(candidate.Username, username) {
			exact = append(exact, candidate)
		}
	}
	switch len(exact) {
	case 0:
		return 0, fmt.Errorf("no user named %q. The API key's user must be able to see them, and they must be a member of project %s", username, projectKey)
	case 1:
		return exact[0].ID, nil
	default:
		return 0, fmt.Errorf("%w: %q matches %d users", errAmbiguousMatch, username, len(exact))
	}
}

// joinNames renders issue type names for an error message.
func joinNames(types []issueTypeDTO) string {
	names := make([]string, 0, len(types))
	for _, t := range types {
		names = append(names, t.Name)
	}
	if len(names) == 0 {
		return "none configured"
	}
	return strings.Join(names, ", ")
}

// rejectEpicIssueType refuses an issue whose type is in the "epic" category.
//
// jirrabit has two things called an epic. The one its project page, roadmap,
// board filter and JQL `epic` read is a separate entity (createJiraEpic), and an
// issue joins it through epic_id. The other is an issue type named "Epic" that
// the seed data ships with. An agent that has driven real Jira files the epic
// as an issue of that type, hangs the work under it with `parent`, and gets an
// epic that appears in no epic list and groups nothing — with no error to say
// so. Refusing it here costs one call and points at the right one.
func rejectEpicIssueType(ctx context.Context, client *jira.Client, payload map[string]any) error {
	id, ok := intValue(payload["issue_type_id"])
	if !ok {
		return nil
	}
	types, err := fetchIssueTypes(ctx, client)
	if err != nil {
		return err
	}
	return epicTypeError(types, id)
}

// errEpicIssueType marks the refusal as this server's own, so it is reported as
// an argument mistake rather than as a failed request to jirrabit.
var errEpicIssueType = errors.New("an epic is not an issue type in jirrabit")

// epicTypeError is rejectEpicIssueType's decision, separate so it can be tested
// without an instance.
func epicTypeError(types []issueTypeDTO, id int) error {
	for _, candidate := range types {
		if candidate.ID == id && strings.EqualFold(candidate.Category, "epic") {
			return fmt.Errorf(
				"%w: an issue of type %q would appear in no epic list and group nothing. "+
					"Create the epic with createJiraEpic, then file each child with "+
					"createJiraIssue(epicId=<its id>) — not parent, which makes a subtask of an issue",
				errEpicIssueType, candidate.Name)
		}
	}
	return nil
}

// intValue reads an id that may have come from a typed argument (int) or from
// the free-form `fields` object, where JSON decoding leaves a float64.
func intValue(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case float64:
		return int(v), v == float64(int(v))
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	}
	return 0, false
}
