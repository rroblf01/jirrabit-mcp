package jira

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// jirrabit paginates by offset (`page`, `size`); Jira's search tool paginates
// with an opaque `nextPageToken`. This file bridges the two so the tool
// contract an agent expects holds even though the backend is offset-based.
//
// The token is a base64url-encoded JSON position. Two properties follow from
// that, and both are worth knowing:
//
//   - It is opaque, so callers must not parse it. A caller that does will break
//     when the encoding changes.
//   - It encodes a position, not a snapshot. Issues created or deleted between
//     two calls can shift a page. That matches Jira's own token behaviour, and
//     is acceptable for agent-driven browsing.

// DefaultPageSize and MaxPageSize mirror jirrabit's own limits
// (jirrabit/api.py) so the adapter does not request sizes the API rejects.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// cursor is the decoded position inside a nextPageToken.
type cursor struct {
	Page int `json:"p"`
	Size int `json:"s"`
}

// CursorFromPage renders jirrabit's 1-based page number as a token.
func CursorFromPage(page int) string {
	if page <= 0 {
		return ""
	}
	raw, err := json.Marshal(cursor{Page: page, Size: DefaultPageSize})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeCursor turns a token back into a 1-based page number. An empty token
// means the first page. A malformed token is an error rather than a silent
// reset to page 1: silently restarting would make a paging loop never end.
func DecodeCursor(token string) (int, error) {
	if token == "" {
		return 1, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		// Tolerate padded base64 too; some clients round-trip it.
		raw, err = base64.URLEncoding.DecodeString(token)
		if err != nil {
			return 0, fmt.Errorf("nextPageToken is not a valid cursor: %w", err)
		}
	}
	var decoded cursor
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return 0, fmt.Errorf("nextPageToken is not a valid cursor: %w", err)
	}
	if decoded.Page < 1 {
		return 0, errors.New("nextPageToken refers to an invalid page")
	}
	return decoded.Page, nil
}

// NextToken returns the token for the following page, or "" when the result set
// is exhausted. page is jirrabit's `next` field: already nil on the last page.
func NextToken(page Page) string {
	if page.Next == nil {
		return ""
	}
	return CursorFromPage(*page.Next)
}

// WithPage appends jirrabit's offset pagination params to a query string that
// already carries the tool's filters.
//
// query is the path with any existing query string already appended, e.g.
// "projects/DEMO/issues/?status=Open". The separator is '?' when there is no
// query yet and '&' otherwise — building "projects/&page=1" would make
// jirrabit answer with a 301 redirect to add the '?'.
func WithPage(query string, page, size int) string {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = DefaultPageSize
	}
	if size > MaxPageSize {
		size = MaxPageSize
	}
	separator := "?"
	if strings.Contains(query, "?") {
		separator = "&"
	}
	return fmt.Sprintf("%s%spage=%d&size=%d", query, separator, page, size)
}
