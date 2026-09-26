package tools

import (
	"context"
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

// issueTypeDTO is the subset of jirrabit's issue-type metadata this adapter
// uses.
type issueTypeDTO struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

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
