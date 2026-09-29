package tools

import (
	"context"
	"net/http"
	"strings"
)

// Per-call credentials travel in the tool arguments, but a client talking to
// this server over HTTP can also send them once as headers instead of repeating
// them on every call:
//
//	X-Jirrabit-Instance-Url: https://jirrabit.example.com
//	X-Jirrabit-Api-Key:      …
//
// Two custom headers rather than Authorization: Bearer, so neither the instance
// nor the key can be mistaken for an OAuth flow by the client, a proxy, or a
// future MCP-layer credential of our own. There is deliberately no equivalent
// over stdio — no headers exist there — where the operator's environment
// (JIRRABIT_URL/JIRRABIT_API_KEY) is the way to name a default instance.
//
// Header credentials are caller-supplied exactly like argument credentials: the
// URL goes through the same SSRF and allowlist checks, and explicit arguments
// still win, so one registration can serve many instances and pin none.

const (
	// HeaderInstanceURL names the jirrabit instance the call is for.
	HeaderInstanceURL = "X-Jirrabit-Instance-Url"
	// HeaderAPIKey carries the API key for that instance.
	HeaderAPIKey = "X-Jirrabit-Api-Key"
)

// headerCredentials is what the HTTP layer extracted, if anything.
type headerCredentials struct {
	url string
	key string
}

type headerCredentialsKey struct{}

// HTTPContextFunc is registered on the streamable HTTP server. It runs per
// request, before any tool handler, and stashes a trimmed copy of the two
// credential headers in the context for Deps.target to fall back on.
func HTTPContextFunc(ctx context.Context, r *http.Request) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, headerCredentialsKey{}, headerCredentials{
		url: strings.TrimSpace(r.Header.Get(HeaderInstanceURL)),
		key: strings.TrimSpace(r.Header.Get(HeaderAPIKey)),
	})
}

// headerTarget returns the credentials the HTTP layer saw, or empty strings
// when the request carried none — stdio requests, or HTTP ones without the
// headers, resolve exactly as before.
func headerTarget(ctx context.Context) (url, key string) {
	creds, _ := ctx.Value(headerCredentialsKey{}).(headerCredentials)
	return creds.url, creds.key
}
