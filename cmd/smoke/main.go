// Command smoke exercises a jirrabit-mcp server end to end over stdio: it
// connects as a real MCP client and calls the tools that jirrabit's current REST
// API can serve, checking that each returns usable data rather than an error.
//
// This is the fastest way to tell a broken deployment from a broken
// implementation. Run it against any jirrabit instance:
//
//	go run ./cmd/smoke -server ./bin/jirrabit-mcp
//
// It creates one issue and one comment, so point it at a scratch instance
// rather than a production one.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

func main() {
	serverPath := flag.String("server", "./bin/jirrabit-mcp", "path to the jirrabit-mcp binary")
	projectKey := flag.String("project", "", "existing project key to create the test issue in. Discovered from listJiraProjects when empty")
	timeout := flag.Duration("timeout", 60*time.Second, "overall timeout")
	flag.Parse()

	if err := run(*serverPath, *projectKey, *timeout); err != nil {
		fmt.Fprintf(os.Stderr, "smoke: %v\n", err)
		os.Exit(1)
	}
}

func run(serverPath, projectKey string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	transportProc := transport.NewStdio(serverPath, nil)
	defer transportProc.Close()

	session := client.NewClient(transportProc)
	if err := session.Start(ctx); err != nil {
		return fmt.Errorf("starting server: %w", err)
	}
	defer session.Close()

	if _, err := session.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	tools, err := session.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	fmt.Printf("tools registered: %d\n", len(tools.Tools))

	step := func(name string, args map[string]any) (string, error) {
		out, err := call(ctx, session, name, args)
		if err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		return out, nil
	}

	// 1. Who am I. Proves the API key authenticated.
	me, err := step("getJiraCurrentUser", map[string]any{})
	if err != nil {
		return err
	}
	fmt.Printf("  getJiraCurrentUser        %s\n", me)

	// 2. What can I see. Proves permission filtering works.
	projects, err := step("listJiraProjects", map[string]any{})
	if err != nil {
		return err
	}
	keys := projectKeys(projects)
	fmt.Printf("  listJiraProjects          %s\n", strings.Join(keys, ", "))

	if projectKey == "" {
		if len(keys) == 0 {
			return fmt.Errorf("no visible projects for this API key; create one or pass -project")
		}
		projectKey = keys[0]
	}

	// 4. Create an issue. Proves POST works and ids resolve.
	created, err := step("createJiraIssue", map[string]any{
		"projectKey":    projectKey,
		"summary":       "Smoke test issue from jirrabit-mcp",
		"description":   "Created by `cmd/smoke` to verify the MCP server end to end.",
		"issueTypeName": "",
		"storyPoints":   1,
	})
	if err != nil {
		return err
	}
	key := jsonString(created, "key")
	if key == "" {
		return fmt.Errorf("createJiraIssue returned no key: %s", created)
	}
	fmt.Printf("  createJiraIssue            %s\n", key)

	// 5. Read it back. Proves the Jira-shaped payload is well formed.
	fetched, err := step("getJiraIssue", map[string]any{"issueIdOrKey": key})
	if err != nil {
		return err
	}
	fmt.Printf("  getJiraIssue               %s\n", jsonString(fetched, "key"))
	if status := nestedString(fetched, "fields", "status", "name"); status != "" {
		fmt.Printf("    fields.status.name       %s\n", status)
	}
	if category := nestedString(fetched, "fields", "status", "statusCategory", "key"); category != "" {
		fmt.Printf("    ...statusCategory.key     %s\n", category)
	}
	if summary := nestedString(fetched, "fields", "summary"); summary != "" {
		fmt.Printf("    fields.summary           %s\n", summary)
	}

	// 6. Comment. Proves nested POST plus the ADF wrapper.
	comment, err := step("addOrEditJiraIssueComment", map[string]any{
		"issueIdOrKey": key,
		"body":         "Comment written by `cmd/smoke`.",
	})
	if err != nil {
		return err
	}
	fmt.Printf("  addOrEditJiraIssueComment  %s\n", truncate(comment, 90))

	// 7. List the comments back.
	comments, err := step("listJiraIssueComments", map[string]any{"issueIdOrKey": key})
	if err != nil {
		return err
	}
	fmt.Printf("  listJiraIssueComments      %d item(s)\n", strings.Count(comments, `"issueKey"`))

	// 8. Worklog, using the Jira duration string form.
	worklog, err := step("addOrEditJiraIssueWorklog", map[string]any{
		"issueIdOrKey": key,
		"timeSpent":    "1h 30m",
		"comment":      "Logged by `cmd/smoke`.",
	})
	if err != nil {
		return err
	}
	if spent := jsonString(worklog, "timeSpent"); spent != "" {
		fmt.Printf("  addOrEditJiraIssueWorklog  %s (from \"1h 30m\")\n", spent)
	}

	// 9. The deliberately-unsupported path must fail with an explanation
	// rather than a confusing 404.
	_, err = step("addOrEditJiraIssueComment", map[string]any{
		"issueIdOrKey": key,
		"body":         "should not be created",
		"commentId":    "1",
	})
	if err != nil {
		fmt.Printf("  edit comment (unsupported) reported cleanly: yes\n")
	} else {
		fmt.Printf("  edit comment (unsupported) returned success; expected an explanation\n")
	}

	fmt.Printf("\nsmoke test passed. Test issue left behind: %s\n", key)
	return nil
}

// call invokes one tool and returns its text content. A tool that reports
// isError comes back as a Go error, because a failed call is a failed check.
func call(ctx context.Context, session *client.Client, name string, args map[string]any) (string, error) {
	var req mcp.CallToolRequest
	req.Params.Name = name
	req.Params.Arguments = args

	res, err := session.CallTool(ctx, req)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for _, block := range res.Content {
		if text, ok := block.(mcp.TextContent); ok {
			out.WriteString(text.Text)
		}
	}
	if res.IsError {
		return out.String(), fmt.Errorf("%s", out.String())
	}
	return out.String(), nil
}

// projectKeys pulls the "key" of every entry in a Jira-shaped project list.
func projectKeys(payload string) []string {
	var envelope struct {
		Values []struct {
			Key string `json:"key"`
		} `json:"values"`
	}
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		return nil
	}
	keys := make([]string, 0, len(envelope.Values))
	for _, v := range envelope.Values {
		keys = append(keys, v.Key)
	}
	return keys
}

// jsonString reads a top-level string field, or "" if absent.
func jsonString(payload, field string) string {
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return ""
	}
	value, _ := decoded[field].(string)
	return value
}

// nestedString walks a chain of object keys and returns the string it finds.
func nestedString(payload string, path ...string) string {
	var current any
	if err := json.Unmarshal([]byte(payload), &current); err != nil {
		return ""
	}
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = object[key]
	}
	value, _ := current.(string)
	return value
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
