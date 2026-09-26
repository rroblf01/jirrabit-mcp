package jira

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ValidateJiraTarget confirms a (url, key) pair addresses a reachable jirrabit
// whose API key works.
//
// This doubles as the guard that keeps a multi-tenant server from becoming an
// open relay: the target must answer jirrabit's own authentication endpoint, so
// a URL that points at some other service fails here with a clear message
// instead of succeeding on a cache miss and confusing every later call.
//
// It costs one extra request per newly-seen instance, and the pool caches the
// result, so it is paid once per instance rather than once per tool call.
func ValidateJiraTarget(ctx context.Context, client *Client) error {
	var user User
	if err := client.Get(ctx, "me/", &user); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			switch {
			case apiErr.StatusCode == 401:
				return fmt.Errorf(
					"the apiKey was rejected by %s (401). Check the key in jirrabit under your profile, "+
						"and that it is not revoked", client.BaseURL())
			case apiErr.NotFound():
				return fmt.Errorf(
					"%s answered, but not with a jirrabit API: /api/v1/me/ returned 404. "+
						"Check the URL points at the jirrabit instance and not a proxy or a path prefix",
					client.BaseURL())
			}
		}
		return fmt.Errorf("could not verify %s as a jirrabit instance: %w", client.BaseURL(), err)
	}
	if strings.TrimSpace(user.Username) == "" {
		return fmt.Errorf(
			"%s answered /api/v1/me/ with no username, so it is not a jirrabit instance",
			client.BaseURL())
	}
	return nil
}
