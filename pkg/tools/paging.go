package tools

import (
	"fmt"

	"github.com/rroblf01/jirrabit-mcp/pkg/jira"
)

// paged is the list envelope returned by list-shaped tools. It carries both
// Jira's `values` key and the `nextPageToken` cursor so a paging loop written
// against Atlassian's server works unchanged, while `startAt` stays available
// for callers that would rather not thread a cursor around.
type paged struct {
	Count         int    `json:"total"`
	StartAt       int    `json:"startAt"`
	MaxResults    int    `json:"maxResults"`
	NextPageToken string `json:"nextPageToken,omitempty"`
	Values        any    `json:"values"`
}

// resolvePage reconciles the two ways a caller can ask for a page: an explicit
// 1-based startAt, or the cursor from a previous response.
//
// A cursor wins when both are present, because a cursor is what a paging loop
// hands back and startAt is what a human types. A malformed cursor is an error
// rather than a silent reset to page 1, because silently restarting would make
// a naive paging loop run forever.
func resolvePage(startAt int, nextPageToken string) (int, error) {
	if nextPageToken != "" {
		page, err := jira.DecodeCursor(nextPageToken)
		if err != nil {
			return 0, fmt.Errorf("invalid nextPageToken: %w", err)
		}
		return page, nil
	}
	if startAt < 1 {
		return 1, nil
	}
	return startAt, nil
}

// clampSize keeps a requested page size within jirrabit's accepted range, so a
// caller asking for 5000 gets 200 items instead of a 422.
func clampSize(requested int) int {
	if requested < 1 {
		return jira.DefaultPageSize
	}
	if requested > jira.MaxPageSize {
		return jira.MaxPageSize
	}
	return requested
}
