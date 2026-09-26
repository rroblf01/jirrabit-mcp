// Command flowtest drives every registered tool over a real MCP stdio session
// and asserts on the shape of each response.
//
// Where cmd/smoke asks "does this return usable data?", flowtest asks "does this
// do the thing an agent would expect?" — that a created issue is readable back,
// that a status change is reflected in getJiraIssue, that the negative cases
// come back as explanations rather than as empty success. The write side is
// verified separately from the Django side, so a tool that returns 200 while
// silently dropping the write cannot pass.
//
//	go run ./cmd/flowtest -server ./bin/jirrabit-mcp
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

var results []string
var failures int

func check(name string, ok bool, detail string) {
	mark := "PASS"
	if !ok {
		mark = "FAIL"
		failures++
	}
	line := fmt.Sprintf("  %s  %s", mark, name)
	if detail != "" && !ok {
		line += "  -- " + detail
	}
	fmt.Println(line)
	results = append(results, fmt.Sprintf("%s %s", mark, name))
}

func main() {
	serverPath := flag.String("server", "./bin/jirrabit-mcp", "path to the server binary")
	timeout := flag.Duration("timeout", 120*time.Second, "overall timeout")
	// -phantoms-only checks the tool prose and nothing else. It needs a server
	// that starts and lists its tools, but no jirrabit behind it, so CI can run
	// it without standing up the whole stack. The full run needs a live instance.
	phantomsOnly := flag.Bool("phantoms-only", false,
		"only check that the instructions and tool descriptions name no unregistered tool")
	flag.Parse()

	if *phantomsOnly {
		if err := checkPhantoms(*serverPath, *timeout); err != nil {
			fmt.Fprintf(os.Stderr, "flowtest: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\n=== phantom check: %d checks, %d failed ===\n", len(results), failures)
		if failures > 0 {
			os.Exit(1)
		}
		return
	}

	if err := run(*serverPath, *timeout); err != nil {
		fmt.Fprintf(os.Stderr, "flowtest: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\n=== flowtest: %d checks, %d failed ===\n", len(results), failures)
	if failures > 0 {
		os.Exit(1)
	}
}

func run(serverPath string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	proc := transport.NewStdio(serverPath, nil)
	defer proc.Close()

	session := client.NewClient(proc)
	if err := session.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	defer session.Close()

	init, err := session.Initialize(ctx, mcp.InitializeRequest{})
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	check("initialize returns server instructions", len(init.Instructions) > 200,
		fmt.Sprintf("instructions are %d bytes", len(init.Instructions)))

	tools, err := session.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	names := map[string]bool{}
	for _, t := range tools.Tools {
		names[t.Name] = true
	}

	if err := scanPhantoms(init.Instructions, tools.Tools); err != nil {
		return err
	}
	check("all 16 default tools are registered", len(tools.Tools) >= 16,
		fmt.Sprintf("only %d registered", len(tools.Tools)))
	fmt.Printf("  tools: %s\n", strings.Join(toolNames(tools.Tools), ", "))

	// --- read side ------------------------------------------------------
	fmt.Println("\n[1] identity and metadata")
	me, err := call(ctx, session, "getJiraCurrentUser", nil)
	check("getJiraCurrentUser", err == nil && jsonString(me, "accountId") != "", errStr(err))
	check("identity is the demo superuser", jsonString(me, "accountId") == "alice_pm",
		jsonString(me, "accountId"))

	projects, err := call(ctx, session, "listJiraProjects", nil)
	check("listJiraProjects", err == nil && strings.Contains(projects, "DEMO"), errStr(err))

	types, err := call(ctx, session, "listJiraIssueTypeMetadata", nil)
	check("listJiraIssueTypeMetadata returns ids", err == nil && strings.Contains(types, "id"),
		errStr(err))
	statuses, err := call(ctx, session, "listJiraStatuses", nil)
	check("listJiraStatuses returns ids", err == nil && strings.Contains(statuses, "\"id\""),
		errStr(err))
	priorities, err := call(ctx, session, "listJiraPriorities", nil)
	check("listJiraPriorities returns ids", err == nil && strings.Contains(priorities, "id"),
		errStr(err))

	// --- the write/read round trip -------------------------------------
	fmt.Println("\n[2] create, read back, edit")
	projectKey := "DEMO"
	issueTypeID := firstID(types)
	statusID := firstID(statuses)
	priorityID := firstID(priorities)

	created, err := call(ctx, session, "createJiraIssue", map[string]any{
		"projectKey":  projectKey,
		"summary":     "Flujo MCP: incidencia de verificacion",
		"description": "Creada por cmd/flowtest.",
		"issueTypeId": issueTypeID,
		"priorityId":  priorityID,
	})
	check("createJiraIssue", err == nil, errStr(err))
	key := jsonString(created, "key")
	check("created issue has a key", strings.HasPrefix(key, projectKey+"-"), key)
	check("created issue carries its summary",
		nestedString(created, "fields", "summary") == "Flujo MCP: incidencia de verificacion",
		truncate(created, 120))

	fetched, err := call(ctx, session, "getJiraIssue", map[string]any{"issueIdOrKey": key})
	check("getJiraIssue reads it back", err == nil, errStr(err))
	check("read-back matches what was written",
		nestedString(fetched, "fields", "summary") == "Flujo MCP: incidencia de verificacion",
		truncate(fetched, 120))

	edited, err := call(ctx, session, "editJiraIssue", map[string]any{
		"issueIdOrKey": key,
		"summary":      "Flujo MCP: resumen editado",
	})
	check("editJiraIssue", err == nil, errStr(err))
	after, _ := call(ctx, session, "getJiraIssue", map[string]any{"issueIdOrKey": key})
	check("edit is visible on re-read",
		nestedString(after, "fields", "summary") == "Flujo MCP: resumen editado",
		truncate(after, 120))
	_ = edited

	fmt.Println("\n[3] status transitions")
	moved, err := call(ctx, session, "editJiraIssue", map[string]any{
		"issueIdOrKey": key,
		"fields":       map[string]any{"status_id": statusID},
	})
	check("editJiraIssue can set status through fields", err == nil, errStr(err))
	after, _ = call(ctx, session, "getJiraIssue", map[string]any{"issueIdOrKey": key})
	check("status change is reflected in getJiraIssue",
		nestedString(after, "fields", "status", "id") == strconv.Itoa(statusID),
		truncate(after, 160))
	_ = moved

	// A status id that does not exist must be refused, not silently ignored.
	_, err = call(ctx, session, "editJiraIssue", map[string]any{
		"issueIdOrKey": key, "fields": map[string]any{"status_id": 999999},
	})
	check("a bogus status_id is rejected", err != nil, "it was accepted")

	// transitionJiraIssue is the name an agent reaches for; both spellings of
	// the destination have to work.
	byID, err := call(ctx, session, "transitionJiraIssue", map[string]any{
		"issueIdOrKey": key, "statusId": statusID,
	})
	check("transitionJiraIssue by id", err == nil, errStr(err))
	byName, err := call(ctx, session, "transitionJiraIssue", map[string]any{
		"issueIdOrKey": key, "statusName": firstStatusName(statuses),
	})
	check("transitionJiraIssue by name", err == nil, errStr(err))
	after, _ = call(ctx, session, "getJiraIssue", map[string]any{"issueIdOrKey": key})
	check("transitionJiraIssue is reflected in getJiraIssue",
		nestedString(after, "fields", "status", "name") == firstStatusName(statuses),
		truncate(after, 200))
	_, _ = byID, byName
	_, err = call(ctx, session, "transitionJiraIssue", map[string]any{
		"issueIdOrKey": key, "statusName": "No Such Status",
	})
	check("transitionJiraIssue with an unknown name lists the real ones",
		err != nil && strings.Contains(err.Error(), "This instance has"),
		fmt.Sprint(err))
	_, err = call(ctx, session, "transitionJiraIssue", map[string]any{"issueIdOrKey": key})
	check("transitionJiraIssue needs a destination", err != nil, "it was accepted")

	// An unknown field name must be refused rather than dropped on the floor.
	_, err = call(ctx, session, "editJiraIssue", map[string]any{
		"issueIdOrKey": key, "fields": map[string]any{"nope": "x"},
	})
	check("an unknown field is rejected, not silently dropped", err != nil,
		"editJiraIssue accepted a field it cannot map")

	fmt.Println("\n[4] comments, worklogs, links, watchers")
	_, err = call(ctx, session, "addOrEditJiraIssueComment", map[string]any{
		"issueIdOrKey": key, "body": "Comentario desde flowtest.",
	})
	check("addOrEditJiraIssueComment", err == nil, errStr(err))
	comments, err := call(ctx, session, "listJiraIssueComments", map[string]any{"issueIdOrKey": key})
	check("listJiraIssueComments finds it", err == nil && strings.Contains(comments, "flowtest"),
		errStr(err))

	worklog, err := call(ctx, session, "addOrEditJiraIssueWorklog", map[string]any{
		"issueIdOrKey": key, "timeSpent": "1h 15m",
	})
	check("addOrEditJiraIssueWorklog", err == nil, errStr(err))
	check("worklog reports the duration in Jira's format",
		strings.Contains(worklog, "1h 15m") && strings.Contains(worklog, "4500"),
		truncate(worklog, 160))
	worklogs, err := call(ctx, session, "listJiraIssueWorklogs", map[string]any{"issueIdOrKey": key})
	check("listJiraIssueWorklogs finds it", err == nil && strings.Contains(worklogs, "timeSpent"),
		errStr(err)+" "+truncate(worklogs, 200))
	check("the worklog is in Jira's time format", strings.Contains(worklogs, "4500"),
		"1h 15m should be 4500 timeSpentSeconds: "+truncate(worklogs, 200))

	// A real second issue, taken from the instance rather than invented: the
	// link tool needs an issue key on both ends.
	other := ""
	if listing, err := call(ctx, session, "searchJiraIssuesUsingJql", map[string]any{
		"jql": fmt.Sprintf("project = %s ORDER BY created DESC", projectKey),
	}); err == nil {
		other = someKeyOtherThan(listing, key)
	}
	check("found a second issue to link against", other != "", "")
	linkTypes, err := call(ctx, session, "listJiraIssueLinkTypes", nil)
	check("listJiraIssueLinkTypes", err == nil && strings.Contains(linkTypes, "relates_to"),
		errStr(err))
	link, err := call(ctx, session, "createJiraIssueLink", map[string]any{
		"linkTypeName": "relates_to", "outwardIssueKey": key, "inwardIssueKey": other,
	})
	check("createJiraIssueLink", err == nil, errStr(err))
	// Jira names the ends outwardIssue/inwardIssue and identifies each by key.
	// The API used to hand back bare numeric ids here, which an agent cannot act
	// on: there is no way to turn 31 back into DEMO-19.
	check("the link is in Jira's shape", strings.Contains(link, "outwardIssue") &&
		strings.Contains(link, "inwardIssue") && strings.Contains(link, `"name":"relates_to"`),
		truncate(link, 200))
	check("both ends are issue keys", strings.Contains(link, `"key":"`+key+`"`) &&
		strings.Contains(link, `"key":"`+other+`"`), truncate(link, 200))
	links, err := call(ctx, session, "getJiraIssueLinks", map[string]any{"issueIdOrKey": key})
	check("getJiraIssueLinks reads it back", err == nil && strings.Contains(links, other),
		errStr(err)+" "+truncate(links, 200))
	check("the read-back is keyed too", strings.Contains(links, `"outwardIssue"`),
		truncate(links, 200))

	for _, watching := range []bool{true, false} {
		if _, err := call(ctx, session, "watchJiraIssue", map[string]any{
			"issueIdOrKey": key, "isWatching": watching,
		}); err != nil {
			check(fmt.Sprintf("watchJiraIssue(%v)", watching), false, err.Error())
		}
	}
	check("watch and unwatch both succeed", true, "")

	fmt.Println("\n[5] JQL search")
	found, err := call(ctx, session, "searchJiraIssuesUsingJql", map[string]any{
		"jql": fmt.Sprintf("project = %s AND text ~ \"flujo\"", projectKey),
	})
	check("searchJiraIssuesUsingJql with ~", err == nil && strings.Contains(found, key),
		errStr(err)+" "+truncate(found, 140))
	total, err := call(ctx, session, "searchJiraIssuesUsingJql", map[string]any{
		"jql": fmt.Sprintf("project = %s", projectKey),
	})
	check("plain project query returns rows", err == nil && strings.Contains(total, "total"),
		errStr(err))
	_, err = call(ctx, session, "searchJiraIssuesUsingJql", map[string]any{"jql": "statuss = Done"})
	check("malformed JQL is explained, not returned as empty", err != nil,
		"it returned no error")
	empty, err := call(ctx, session, "searchJiraIssuesUsingJql", map[string]any{
		"jql": fmt.Sprintf("project = %s AND text = \"nada de esto\"", projectKey),
	})
	check("a no-match JQL returns an empty result, not an error", err == nil, errStr(err))
	check("the empty result really is empty", strings.Contains(empty, "\"total\":0") ||
		strings.Contains(empty, `"values":[]`), truncate(empty, 120))

	fmt.Println("\n[6] negative cases")
	_, err = call(ctx, session, "getJiraIssue", map[string]any{"issueIdOrKey": "DEMO-99999"})
	check("a missing issue is a clear error", err != nil && strings.Contains(err.Error(), "404"),
		fmt.Sprint(err))
	_, err = call(ctx, session, "getJiraIssue", map[string]any{"issueIdOrKey": ""})
	check("an empty issue key is rejected", err != nil, "it was accepted")
	_, err = call(ctx, session, "createJiraIssue", map[string]any{
		"projectKeyOrId": "NOPROJECT", "summary": "x", "issueTypeId": issueTypeID,
	})
	check("creating in a missing project is a clear error", err != nil, "it was accepted")

	fmt.Println("\n[7] multi-tenancy isolation")
	// The same server, a second instance identity: the token belongs to one
	// instance, so pointing it at a dead host must fail rather than fall back
	// to the default instance.
	_, err = callWith(ctx, session, map[string]any{
		"instanceUrl": "http://127.0.0.1:9/", "apiKey": "not-a-real-key",
		"cloudId": "", "projectKey": projectKey, "summary": "no debe existir",
		"issueTypeId": issueTypeID,
	}, "createJiraIssue")
	check("a dead instanceUrl fails instead of falling back to the default",
		err != nil, "it succeeded against the default instance")

	// The opt-in tools are checked both ways: absent without their flag,
	// present and working with it. Registering is not the same as working.
	fmt.Println("\n[7b] sprints, saved filters and users")
	sprints, err := call(ctx, session, "listJiraSprints", map[string]any{
		"projectKeyOrId": projectKey,
	})
	check("listJiraSprints", err == nil && strings.Contains(sprints, "values"),
		errStr(err)+" "+truncate(sprints, 160))
	firstSprint := firstID(sprints)
	one, err := call(ctx, session, "getJiraSprint", map[string]any{"sprintId": firstSprint})
	check("getJiraSprint", err == nil && strings.Contains(one, "name"),
		errStr(err)+" "+truncate(one, 160))
	_, err = call(ctx, session, "getJiraSprint", map[string]any{})
	check("getJiraSprint needs an id", err != nil, "it was accepted")

	createdSprint, err := call(ctx, session, "createJiraSprint", map[string]any{
		"projectKey": projectKey, "name": "flowtest sprint",
		"goal": "created by flowtest", "startDate": "2026-10-01", "endDate": "2026-10-14",
	})
	check("createJiraSprint", err == nil && strings.Contains(createdSprint, "flowtest sprint"),
		errStr(err)+" "+truncate(createdSprint, 160))
	newSprintID := firstID(createdSprint)
	check("the new sprint comes back as future", strings.Contains(createdSprint, `"status":"future"`),
		truncate(createdSprint, 160))
	updatedSprint, err := call(ctx, session, "updateJiraSprint", map[string]any{
		"sprintId": newSprintID, "goal": "updated by flowtest",
	})
	check("updateJiraSprint", err == nil && strings.Contains(updatedSprint, "updated by flowtest"),
		errStr(err)+" "+truncate(updatedSprint, 160))
	afterSprint, _ := call(ctx, session, "getJiraSprint", map[string]any{"sprintId": newSprintID})
	check("the sprint update is visible on re-read",
		strings.Contains(afterSprint, "updated by flowtest"), truncate(afterSprint, 160))
	_, err = call(ctx, session, "createJiraSprint", map[string]any{
		"projectKey": projectKey, "name": "x", "startDate": "not-a-date",
	})
	check("a malformed startDate is rejected before the request", err != nil, "it was accepted")
	_, err = call(ctx, session, "createJiraSprint", map[string]any{"projectKey": projectKey})
	check("createJiraSprint needs a name", err != nil, "it was accepted")
	_, err = call(ctx, session, "createJiraSprint", map[string]any{
		"projectKey": "NOPROJECT", "name": "x",
	})
	check("creating a sprint in a missing project fails", err != nil, "it was accepted")

	filters, err := call(ctx, session, "listJiraSavedFilters", nil)
	check("listJiraSavedFilters", err == nil && strings.Contains(filters, "values"),
		errStr(err)+" "+truncate(filters, 160))

	user, err := call(ctx, session, "getJiraUser", map[string]any{"userIdOrKey": "alice_pm"})
	check("getJiraUser by username", err == nil && strings.Contains(user, "alice_pm"),
		errStr(err)+" "+truncate(user, 160))
	byID, err2 := call(ctx, session, "getJiraUser", map[string]any{"userIdOrKey": "1"})
	check("getJiraUser by numeric id", err2 == nil && strings.Contains(byID, "alice_pm"),
		errStr(err2)+" "+truncate(byID, 160))
	_, err = call(ctx, session, "getJiraUser", map[string]any{"userIdOrKey": "nobody_at_all"})
	check("getJiraUser on an unknown name explains itself", err != nil &&
		strings.Contains(err.Error(), "no user named"), fmt.Sprint(err))

	fmt.Println("\n[8] opt-in destructive tools")
	// Assert against the flag actually in force, so the run is meaningful in
	// both modes instead of only when the flags happen to be unset.
	flags := map[string]string{
		"deleteJiraIssue":   "JIRRABIT_MCP_ENABLE_DELETE",
		"updateJiraProject": "JIRRABIT_MCP_ENABLE_MANAGE",
	}
	for tool, flag := range flags {
		_, registered := names[tool]
		want := truthy(os.Getenv(flag))
		check(fmt.Sprintf("%s is registered iff %s is on", tool, flag), registered == want,
			fmt.Sprintf("registered=%v but %s=%q", registered, flag, os.Getenv(flag)))
	}
	if names["deleteJiraIssue"] {
		deletedSprint, err := call(ctx, session, "deleteJiraSprint", map[string]any{
			"sprintId": newSprintID,
		})
		check("deleteJiraSprint", err == nil, errStr(err)+" "+truncate(deletedSprint, 120))
		_, err = call(ctx, session, "getJiraSprint", map[string]any{"sprintId": newSprintID})
		check("the deleted sprint is really gone", err != nil && strings.Contains(err.Error(), "404"),
			fmt.Sprint(err))
	}

	if names["deleteJiraIssue"] && names["updateJiraProject"] {
		fmt.Println("\n[8b] the same tools, exercised because their flags are on")
		doomed, err := call(ctx, session, "createJiraIssue", map[string]any{
			"projectKey": projectKey, "summary": "flowtest: se va a borrar",
			"issueTypeId": issueTypeID,
		})
		check("created a throwaway issue to delete", err == nil, errStr(err))
		doomedKey := jsonString(doomed, "key")

		renamed, err := call(ctx, session, "updateJiraProject", map[string]any{
			"projectKeyOrId": projectKey, "name": "DEMO (flowtest)",
		})
		check("updateJiraProject renames the project", err == nil, errStr(err))
		check("the new name comes back in the payload",
			strings.Contains(renamed, "flowtest"), truncate(renamed, 160))
		back, err := call(ctx, session, "listJiraProjects", nil)
		check("the rename is visible in listJiraProjects",
			err == nil && strings.Contains(back, "flowtest"), errStr(err))
		// leave the demo looking like itself again
		if _, err := call(ctx, session, "updateJiraProject", map[string]any{
			"projectKeyOrId": projectKey, "name": "Demo project",
		}); err != nil {
			check("restoring the project name", false, err.Error())
		} else {
			check("restoring the project name", true, "")
		}

		if _, err := call(ctx, session, "deleteJiraIssue", map[string]any{
			"issueIdOrKey": doomedKey,
		}); err != nil {
			check("deleteJiraIssue deletes it", false, err.Error())
		} else {
			check("deleteJiraIssue deletes it", true, "")
		}
		_, err = call(ctx, session, "getJiraIssue", map[string]any{"issueIdOrKey": doomedKey})
		check("the deleted issue is really gone", err != nil && strings.Contains(err.Error(), "404"),
			fmt.Sprint(err))
	}

	return nil
}

// toolNamePattern matches the shape of a tool name: a Jira-style verb followed
// by "Jira" and a subject. It is deliberately loose, because the cost of a
// false positive is a red test and the cost of a miss is a broken agent.
var toolNamePattern = regexp.MustCompile(
	`\b(?:get|list|create|edit|add|delete|update|transition|search|watch|assign|archive|clone|restore)Jira[A-Za-z]*\b`)

// optInTools are registered only when their feature flag is set, and the
// instructions say so. Their absence is the point, so they are not phantoms.
var optInTools = map[string]bool{"deleteJiraIssue": true, "updateJiraProject": true}

// scanPhantoms reports any tool name mentioned in the server instructions or in
// a tool description that is not actually registered.
//
// This was not hypothetical: editJiraIssue's description told agents to use
// transitionJiraIssue "to change status", and the README repeated it, while
// no such tool existed. An agent following that sentence got "tool not found"
// and had no way to recover. Nothing caught it because no test read the prose.
func scanPhantoms(instructions string, tools []mcp.Tool) error {
	registered := map[string]bool{}
	for _, t := range tools {
		registered[t.Name] = true
	}
	phantoms := map[string]string{}
	scan := func(where, text string) {
		for _, candidate := range toolNamePattern.FindAllString(text, -1) {
			if !registered[candidate] && !optInTools[candidate] {
				phantoms[candidate] = where
			}
		}
	}
	scan("server instructions", instructions)
	for _, t := range tools {
		scan("description of "+t.Name, t.Description)
	}
	if len(phantoms) == 0 {
		check("no phantom tool names in the instructions or any description", true, "")
		return nil
	}
	for name, where := range phantoms {
		check("the instructions must not name an unregistered tool: "+name, false,
			"mentioned in "+where)
	}
	return nil
}

// checkPhantoms runs only the prose check, against a server with no jirrabit
// behind it. Starting the server and listing its tools need no backend, which
// is what lets CI run this without the full stack.
func checkPhantoms(serverPath string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	proc := transport.NewStdio(serverPath, nil)
	defer proc.Close()

	session := client.NewClient(proc)
	if err := session.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	defer session.Close()

	init, err := session.Initialize(ctx, mcp.InitializeRequest{})
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	tools, err := session.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	fmt.Printf("checking %d tools and %d bytes of instructions\n",
		len(tools.Tools), len(init.Instructions))
	return scanPhantoms(init.Instructions, tools.Tools)
}

// callWith lets a test override instanceUrl/apiKey, which the per-call fields
// exist for.
func callWith(ctx context.Context, session *client.Client, args map[string]any, name string) (string, error) {
	var req mcp.CallToolRequest
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := session.CallTool(ctx, req)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for _, b := range res.Content {
		if t, ok := b.(mcp.TextContent); ok {
			out.WriteString(t.Text)
		}
	}
	if res.IsError {
		return out.String(), fmt.Errorf("%s", out.String())
	}
	return out.String(), nil
}

func call(ctx context.Context, session *client.Client, name string, args map[string]any) (string, error) {
	return callWith(ctx, session, args, name)
}

func toolNames(tools []mcp.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}

// firstID pulls the first row's numeric id out of a metadata response. jirrabit
// returns ids as JSON numbers, and CreateIssueArgs.issueTypeId is an int, so
// handing this over as a string is a type error rather than a lookup.
func firstID(payload string) int {
	var decoded map[string]any
	if json.Unmarshal([]byte(payload), &decoded) != nil {
		return 0
	}
	// A single resource is a bare object; a list is wrapped in values/items.
	if _, ok := decoded["id"]; ok {
		decoded = map[string]any{"values": []any{decoded}}
	}
	rows, _ := decoded["values"].([]any)
	if len(rows) == 0 {
		rows, _ = decoded["items"].([]any)
	}
	if len(rows) == 0 {
		return 0
	}
	first, _ := rows[0].(map[string]any)
	switch id := first["id"].(type) {
	case float64:
		return int(id)
	case string:
		parsed, _ := strconv.Atoi(id)
		return parsed
	}
	return 0
}

// firstStatusName reads the first status name out of a metadata response.
func firstStatusName(payload string) string {
	var decoded map[string]any
	if json.Unmarshal([]byte(payload), &decoded) != nil {
		return ""
	}
	rows, _ := decoded["values"].([]any)
	if len(rows) == 0 {
		rows, _ = decoded["items"].([]any)
	}
	if len(rows) == 0 {
		return ""
	}
	first, _ := rows[0].(map[string]any)
	name, _ := first["name"].(string)
	return name
}

// someKeyOtherThan returns the first issue key in a search result that is not
// the excluded one.
func someKeyOtherThan(payload, exclude string) string {
	var envelope struct {
		Values []struct {
			Key string `json:"key"`
		} `json:"values"`
	}
	if json.Unmarshal([]byte(payload), &envelope) != nil {
		return ""
	}
	for _, v := range envelope.Values {
		if v.Key != "" && v.Key != exclude {
			return v.Key
		}
	}
	return ""
}

func jsonString(payload, field string) string {
	var decoded map[string]any
	if json.Unmarshal([]byte(payload), &decoded) != nil {
		return ""
	}
	v, _ := decoded[field].(string)
	return v
}

func nestedString(payload string, path ...string) string {
	var current any
	if json.Unmarshal([]byte(payload), &current) != nil {
		return ""
	}
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = object[key]
	}
	v, _ := current.(string)
	return v
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// truthy mirrors the server's own BoolEnv parsing so the test agrees with it.
func truthy(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
