// Package tools registers the Jira-vocabulary MCP tools against a jirrabit
// instance. One file per Atlassian tool group, mirroring the reference server's
// layout so a reader can compare the two directly.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/rroblf01/jirrabit-mcp/pkg/jira"
	"github.com/rroblf01/jirrabit-mcp/pkg/schema"
)

// Deps is what every tool handler needs: a way to reach the instance a call
// names, and to turn that instance's DTOs into Jira payloads.
//
// Resolution is per call, so one deployed server can serve many jirrabit
// instances; the pool caches clients so that costs nothing in practice.
type Deps struct {
	Pool *jira.Pool
}

// target resolves the instance a call is about and returns a matching Shaper.
// Handlers call this first and use the returned shaper rather than a shared one,
// because `self` links must point at the instance the data came from.
func (d Deps) target(ctx context.Context, args interface {
	GetTarget() (string, string)
}) (*jira.Client, *jira.Shaper, error) {
	url, key := args.GetTarget()
	client, err := d.Pool.Resolve(ctx, url, key)
	if err != nil {
		return nil, nil, err
	}
	return client, d.Pool.ShaperFor(client), nil
}

// Options toggles the tool groups that are off by default.
type Options struct {
	// EnableDelete registers the destructive tools. Off by default: an agent
	// should have to be told that permanent deletion is available.
	EnableDelete bool
	// EnableManage registers project administration. Off by default.
	EnableManage bool
}

// Register wires every enabled tool group onto the MCP server.
func (d Deps) Register(s *server.MCPServer, opts Options) {
	registerReadTools(s, d)
	registerMetadataTools(s, d)
	registerSearchTools(s, d)
	registerLinkTools(s, d)
	registerWatchTools(s, d)
	registerIssueWriteTools(s, d)

	if opts.EnableDelete {
		registerDeleteTools(s, d)
	}
	if opts.EnableManage {
		registerManageTools(s, d)
	}
}

// jsonResult marshals a value as compact JSON and returns it as tool text.
//
// Compact rather than indented: this payload is read by a model, and whitespace
// is tokens spent on nothing.
func jsonResult(value any) (*mcp.CallToolResult, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encoding tool result: %w", err)
	}
	return mcp.NewToolResultText(string(raw)), nil
}

// toolError turns an error into a tool result an agent can act on.
//
// Expected failures (404, 403, invalid input) become tool errors with a
// sentence an agent can reason about. Only genuine bugs reach Go's error path.
func toolError(err error) (*mcp.CallToolResult, error) {
	if err == nil {
		return nil, nil
	}

	var apiErr *jira.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.NotFound():
			return mcp.NewToolResultErrorf(
				"Not found: jirrabit returned 404 for %s %s. Check the key exists, and that the API key's owner is a member of the owning project — jirrabit hides projects the caller cannot see behind a 404 on purpose. (%s)",
				apiErr.Method, apiErr.Path, apiErr.Detail,
			), nil
		case apiErr.Forbidden():
			return mcp.NewToolResultErrorf(
				"Permission denied: the API key's user lacks the required role (admin or lead) for %s %s. (%s)",
				apiErr.Method, apiErr.Path, apiErr.Detail,
			), nil
		case apiErr.Unprocessable():
			return mcp.NewToolResultErrorf(
				"jirrabit rejected the request as invalid (400). %s", apiErr.Detail,
			), nil
		case apiErr.StatusCode == 401:
			return mcp.NewToolResultError(
				"jirrabit rejected the API key (401). Check JIRRABIT_API_KEY: it may be revoked, or it may not match this instance.",
			), nil
		}
	}

	// A transport failure or an unhandled status: log the detail server-side
	// and keep the message short enough to be read by a model.
	log.Printf("[jirrabit-mcp] request failed: %v", err)
	return mcp.NewToolResultErrorf("Request to jirrabit failed: %v", err), nil
}

// BoolEnv reads a boolean environment variable. Anything unrecognised is false,
// so a typo disables a feature rather than enabling it by accident.
func BoolEnv(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// clientFor resolves the instance a call targets from raw arguments, for tools
// whose only inputs are the instance selector.
func (d Deps) clientFor(ctx context.Context, req mcp.CallToolRequest) (*jira.Client, error) {
	var target schema.Target
	if err := req.BindArguments(&target); err != nil {
		return nil, err
	}
	client, _, err := d.target(ctx, target)
	return client, err
}
