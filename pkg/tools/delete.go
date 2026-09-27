package tools

import (
	"context"
	"fmt"
	"log"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/rroblf01/jirrabit-mcp/pkg/schema"
)

// registerDeleteTools wires the destructive tools. They are only registered when
// JIRRABIT_MCP_ENABLE_DELETE is set, so an agent cannot discover — and therefore
// cannot be tempted by — a permanent-delete capability the operator did not
// offer. jirrabit's own permission checks remain the real gate either way.
func registerDeleteTools(s *registrar, d Deps) {
	s.AddTool(mcp.NewTool("deleteJiraIssue",
		mcp.WithDescription("Permanently delete an issue. This cannot be undone: comments, worklogs and attachments go with it. Only available because the operator enabled delete tools."),
		mcp.WithTitleAnnotation("Delete issue"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
	), deleteJiraIssue(d))
}

func deleteJiraIssue(d Deps) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var target schema.Target
		if err := req.BindArguments(&target); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, _, err := d.target(ctx, target)
		if err != nil {
			return toolError(err)
		}
		issueKey := req.GetString("issueIdOrKey", "")
		if issueKey == "" {
			return mcp.NewToolResultError("issueIdOrKey is required, e.g. WEB-1"), nil
		}
		log.Printf("[jirrabit-mcp deleteJiraIssue] %s on %s", issueKey, client.BaseURL())

		path := fmt.Sprintf("issues/%s/", url.PathEscape(issueKey))
		if err := client.Delete(ctx, path, nil); err != nil {
			return toolError(err)
		}
		return jsonResult(map[string]any{
			"deleted": true,
			"key":     issueKey,
		})
	}
}
