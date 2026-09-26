package tools

import (
	"context"
	"strconv"

	"github.com/rroblf01/jirrabit-mcp/pkg/jira"
)

// isProjectKey reports whether an identifier looks like a project key rather
// than a numeric id. jirrabit's project keys are 2-10 uppercase letters,
// optionally preceded by an uppercase letter key, e.g. "WEB" or "A1BC".
func isProjectKey(value string) bool {
	if value == "" || len(value) > 10 {
		return false
	}
	for i := 0; i < len(value); i++ {
		char := value[i]
		isUpper := char >= 'A' && char <= 'Z'
		isDigit := char >= '0' && char <= '9'
		if !isUpper && !(i > 0 && isDigit) {
			return false
		}
	}
	return true
}

// resolveProjectKey turns a numeric project id into its key, so a caller that
// only has the id from a previous response can still address the project.
func resolveProjectKey(ctx context.Context, d Deps, id string) (string, error) {
	projects, _, err := jira.List[jira.Project](ctx, d.Client, "projects/?size=200")
	if err != nil {
		return "", err
	}
	wanted, err := strconv.Atoi(id)
	if err != nil {
		return "", err
	}
	for _, project := range projects {
		if project.ID == wanted {
			return project.Key, nil
		}
	}
	return "", &jira.APIError{
		StatusCode: 404,
		Detail:     "no visible project with id " + id,
		Method:     "GET",
		Path:       "projects/",
	}
}
